package catalog

import (
	"archive/zip"
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"text/template"
	"time"

	"github.com/Masterminds/sprig"
	"github.com/aokoli/goutils"
	"github.com/monostream/helmi/pkg/helm"
	"github.com/monostream/helmi/pkg/kubectl"
	"github.com/satori/go.uuid"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v2"
)

const (
	metadataKey           = "__metadata"
	metadataServiceIdKey  = "helmiServiceId"
	metadataPlanIdKey     = "helmiPlanId"
	metadataIngressDomain = "helmiSvcDomain"
)

type ServiceMap map[string]Service

type Catalog struct {
	services atomic.Value // of type ServiceMap
}

type Service struct {
	Id          string `yaml:"_id"`
	Name        string `yaml:"_name"`
	Description string `yaml:"description"`
	Metadata    map[string]interface{} `yaml:"metadata"`

	Chart        string `yaml:"chart"`
	ChartVersion string `yaml:"chart-version"`

	Plans []Plan `yaml:"plans"`

	valuesTemplate      *template.Template
	credentialsTemplate *template.Template
}

type Plan struct {
	Id          string `yaml:"_id"`
	Name        string `yaml:"_name"`
	Description string `yaml:"description"`
	Metadata    map[string]interface{} `yaml:"metadata"`

	Chart        string                      `yaml:"chart"`
	ChartVersion string                      `yaml:"chart-version"`
	ChartValues  map[interface{}]interface{} `yaml:"chart-values"`

	UserCredentials map[interface{}]interface{} `yaml:"user-credentials"`
}

type Release struct {
	UserCredentials map[string]interface{}
	HealthCheckURLs []string
}

// Parses serialized byte array
func NewFromSerialized(serializedCatalog []byte) (*Catalog, error) {
	c := Catalog{services: atomic.Value{}}
	services := ServiceMap{}
	err := addServiceYaml(services, serializedCatalog, "<no file>")
	if err != nil {
		return nil, err
	}
	c.services.Store(services)

	return &c, nil
}

// Parses any catalog format: local directories, local zip archives or zip archive urls
func New(dirOrZipOrZipUrl string) (*Catalog, error) {
	serviceMap, err := parseAny(dirOrZipOrZipUrl)
	if err != nil {
		return nil, err
	}

	c := &Catalog{ services: atomic.Value{}	}
	c.services.Store(serviceMap)

	catalogUpdateInterval := time.Minute * 15
	if interval, ok := os.LookupEnv("CATALOG_UPDATE_INTERVAL"); ok {
		dur, err := time.ParseDuration(interval)
		if err != nil {
			return nil, fmt.Errorf("invalid env var CATALOG_UPDATE_INTERVAL: %s", err)
		}
		catalogUpdateInterval = dur
	}

	// start go-routine to periodically update the catalog in the background
	go func() {
		for {
			time.Sleep(catalogUpdateInterval)

			err := helm.RepoUpdate()
			if err != nil {
				log.Printf("helm repo update failed: %s", err)
			}

			serviceMap, err := parseAny(dirOrZipOrZipUrl)
			if err != nil {
				log.Printf("failed to update catalog: %s", err)
			} else {
				// update the reference atomically
				c.services.Store(serviceMap)
			}
		}
	}()

	return c, nil
}

func parseAny(dirOrZipOrZipUrl string) (ServiceMap, error) {
	if strings.HasPrefix(dirOrZipOrZipUrl, "http://") || strings.HasPrefix(dirOrZipOrZipUrl, "https://") {
		return parseZipURL(dirOrZipOrZipUrl)
	}

	fi, err := os.Stat(dirOrZipOrZipUrl)
	if err != nil {
		return nil, err
	}

	if fi.IsDir() {
		return parseDir(dirOrZipOrZipUrl)
	}

	return parseZipFile(dirOrZipOrZipUrl)
}

// Parses all `.yaml` and `.yml` files in the specified path as service definitions
func parseDir(dir string) (ServiceMap, error) {
	services := make(map[string]Service)

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("unable to read catalog file: %q: %s", path, err)
			return nil
		}

		ext := filepath.Ext(path)
		if info.IsDir() || (ext != ".yml" && ext != ".yaml") {
			return nil
		}

		input, ioErr := ioutil.ReadFile(path)
		if ioErr != nil {
			return ioErr
		}

		return addServiceYaml(services, input, path)
	})

	if err != nil {
		return nil, err
	}

	if len(services) == 0 {
		err = fmt.Errorf("no services found in catalog directory: %s", dir)
		return nil, err
	}

	return services, nil
}

func parseZipFile(file string) (ServiceMap, error) {
	zipFile, err := zip.OpenReader(file)
	if err != nil {
		return nil, err
	}
	defer zipFile.Close()

	return parseZipReader(&zipFile.Reader, file)
}

func parseZipURL(url string) (ServiceMap, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	reader := bytes.NewReader(b)
	zipReader, err := zip.NewReader(reader, reader.Size())
	if err != nil {
		return nil, err
	}

	return parseZipReader(zipReader, url)
}

func parseZipReader(zipReader *zip.Reader, path string) (ServiceMap, error) {
	services := make(map[string]Service)

	for _, entry := range zipReader.File {
		ext := filepath.Ext(entry.Name)
		if ext != ".yml" && ext != ".yaml" {
			continue
		}

		f, err := entry.Open()
		if err != nil {
			return nil, err
		}

		content, err := ioutil.ReadAll(f)
		f.Close()

		if err != nil {
			return nil, err
		}

		err = addServiceYaml(services, content, path)
		if err != nil {
			return nil, err
		}
	}

	if len(services) == 0 {
		err := fmt.Errorf("no services found in catalog zip file: %s", path)
		return nil, err
	}

	return services, nil
}

func addServiceYaml(services ServiceMap, input []byte, file string) error {
	// we have three documents: service, chart-values, user-credentials
	documents := bytes.Split(input, []byte("\n---"))
	if n := len(documents); n != 3 {
		return fmt.Errorf("service file %s: must contain 3 yaml document parts, found %d", file, n)
	}

	var s struct{ Service }
	err := yaml.Unmarshal(documents[0], &s)
	if err != nil {
		return fmt.Errorf("failed to parse service definition: %s: %s", file, err)
	}

	fMap := templateFuncMap()
	valuesTemplate, valuesErr := template.New("values").Funcs(fMap).Parse(string(documents[1]))
	if valuesErr != nil {
		return fmt.Errorf("failed to parse values template: %s: %s", file, valuesErr)
	}

	credentialsTemplate, credentialsErr := template.New("credentials").Funcs(fMap).Parse(string(documents[2]))
	if credentialsErr != nil {
		return fmt.Errorf("failed to parse credentials template: %s: %s", file, credentialsErr)
	}

	s.valuesTemplate = valuesTemplate
	s.credentialsTemplate = credentialsTemplate

	services[s.Id] = s.Service
	return nil
}

func (c *Catalog) Services() ServiceMap {
	return c.services.Load().(ServiceMap)
}

func (c *Catalog) Service(id string) *Service {
	services := c.Services()
	if val, ok := services[id]; ok {
		return &val
	}
	return nil
}

func (s *Service) Plan(id string) *Plan {
	for _, p := range s.Plans {
		if strings.EqualFold(p.Id, id) {
			return &p
		}
	}
	return nil
}

func templateFuncMap() template.FuncMap {
	f := sprig.TxtFuncMap()

	f["toYaml"] = func(v interface{}) string {
		data, err := yaml.Marshal(v)
		if err != nil {
			return ""
		}
		return string(data)
	}

	f["fromYaml"] = func(str string) map[string]interface{} {
		m := map[string]interface{}{}

		if err := yaml.Unmarshal([]byte(str), &m); err != nil {
			m["Error"] = err.Error()
		}
		return m
	}

	f["htpasswd"] = func(str string) string {
		s := sha1.New()

		s.Write([]byte(str))
		passwordSum := []byte(s.Sum(nil))

		return "{SHA}" + base64.StdEncoding.EncodeToString(passwordSum)
	}

	f["md5sum"] = func(str string) string {
		s := md5.New()

		s.Write([]byte(str))
		md5sum := []byte(s.Sum(nil))

		return fmt.Sprintf("%x", md5sum)
	}

	f["bcrypt"] = func(str string) string {
		bcrypted, _ := bcrypt.GenerateFromPassword([]byte(str), 14)
		return string(bcrypted)
	}

	f["generateUsername"] = func() string {
		s := uuid.NewV4().String()
		s = strings.Replace(s, "-", "", -1)
		return "u" + s[:30]
	}

	f["generatePassword"] = func() string {
		prefix, _ := goutils.RandomAlphabetic(1)
		suffix, _ := goutils.RandomAlphaNumeric(31)
		return prefix + suffix
	}

	f["generateDnsNames"] = func(release string, dnsSuffix string) []string {
		shortLen := 63 - len(dnsSuffix)
		longName := fmt.Sprintf("%s.%s", release, dnsSuffix)
		if shortLen <= 0 || len(longName) <= 64 {
			return []string{longName}
		}

		hash := fmt.Sprintf("%x", sha1.Sum([]byte(release)))
		if shortLen > 8 {
			shortLen = 8
		}
		shortName := fmt.Sprintf("%s.%s", hash[:shortLen], dnsSuffix)
		return []string{shortName, longName}
	}

	return f
}

// Note: yaml.v3 will make this unnecessary by exposing a default map type for unmarshalling:
// https://github.com/go-yaml/yaml/issues/139
func toStringMap(m map[interface{}]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range m {
		// convert key to string
		k := fmt.Sprintf("%v", k)
		if valMap, ok := v.(map[interface{}]interface{}); ok {
			// convert values recursively
			v = toStringMap(valMap)
		}
		result[k] = v
	}

	return result
}

// Merges a list of unmarshalled yaml maps into a single string-index map.
// Conflicting values are merged recursively if they are maps, and overwritten if they are of any other type.
func mergeMaps(maps ...map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for _, m := range maps {
		for k, v := range m {
			valMap, valIsMap := v.(map[string]interface{})
			resMap, resIsMap := result[k].(map[string]interface{})

			if valIsMap && resIsMap {
				v = mergeMaps(resMap, valMap)
			}

			result[k] = v
		}
	}
	return result
}

type chartValueVars struct {
	Service    *Service
	Plan       *Plan
	Parameters map[string]interface{}
	Context    map[string]interface{}
	Release    struct {
		Name string
	}
	Cluster *clusterVars
}

type Metadata struct {
	ServiceId     string
	PlanId        string
	IngressDomain string
}

func ExtractMetadata(helmValues map[string]interface{}) (Metadata, error) {
	metadataMap, ok := helmValues[metadataKey].(map[string]interface{})
	if !ok {
		// yaml.v2 deserializes into map[interface{}]interface{}
		rawMetadataMap, ok := helmValues[metadataKey].(map[interface{}]interface{})
		if !ok {
			return Metadata{}, errors.New("failed to fetch helmi metadata from helm values")
		}

		metadataMap = toStringMap(rawMetadataMap)
	}

	serviceId, hasServiceId := metadataMap[metadataServiceIdKey].(string)
	planId, hasPlanId := metadataMap[metadataPlanIdKey].(string)
	ingressDomain, _ := metadataMap[metadataIngressDomain].(string)

	if !(hasServiceId && hasPlanId) {
		return Metadata{}, errors.New("incomplete helmi metadata in helm values")
	}

	// backwards-compatibility: old releases might not have an ingress domain in metadata
	if len(ingressDomain) == 0 {
		ingressDomain = os.Getenv("INGRESS_DOMAIN")
	}

	metadata := Metadata{
		ServiceId:     serviceId,
		PlanId:        planId,
		IngressDomain: ingressDomain,
	}

	return metadata, nil
}

func (s *Service) ChartValues(p *Plan, releaseName string, namespace kubectl.Namespace, params map[string]interface{}, contextValues map[string]interface{}) (map[string]interface{}, error) {
	b := new(bytes.Buffer)

	// since Cluster.Address and Cluster.Hostname are never used in the ChartValues, errors here aren't handled
	nodes, _ := kubectl.GetNodes()

	data := chartValueVars{
		Service:    s,
		Plan:       p,
		Release:    struct{ Name string }{Name: releaseName},
		Parameters: params,
		Context: contextValues,
		Cluster: &clusterVars{
			Address:       extractAddress(nodes),
			Hostname:      extractHostname(nodes),
			IngressDomain: namespace.IngressDomain,
		},
	}
	err := s.valuesTemplate.Execute(b, data)
	if err != nil {
		return nil, err
	}

	var v struct {
		ChartValues map[interface{}]interface{} `yaml:"chart-values"`
	}

	err = yaml.Unmarshal(b.Bytes(), &v)
	if err != nil {
		return nil, err
	}

	metadata := map[string]interface{}{
		metadataKey: map[string]interface{}{
			metadataServiceIdKey:  s.Id,
			metadataPlanIdKey:     p.Id,
			metadataIngressDomain: namespace.IngressDomain,
		},
	}

	serviceValues := toStringMap(v.ChartValues)
	planValues := toStringMap(p.ChartValues)
	values := mergeMaps(serviceValues, planValues, metadata)

	return values, nil
}

type credentialVars struct {
	Service  *Service
	Plan     *Plan
	Values   valueVars
	Release  releaseVars
	Cluster  *clusterVars
	Services *servicesVars
}

type valueVars map[string]interface{}

type releaseVars struct {
	Name      string
	Namespace string
}

type clusterVars struct {
	Address       string
	Hostname      string
	IngressDomain string
}

type servicesVars struct {
	services map[string]kubectl.Service
	nodes    []kubectl.Node
}

func (s *servicesVars) Address(serviceName string, port int) string {
	svcPort := s.Port(serviceName, port)
	svcIP := s.IP(serviceName)
	if len(svcPort) > 0 && len(svcIP) > 0 {
		return svcIP + ":" + svcPort
	}

	return ""
}

func mapPort(svc kubectl.Service, port int) (int, bool) {
	switch svc.Type {
	case "NodePort":
		if nodePort, ok := svc.NodePorts[port]; ok {
			// return the mapped port
			return nodePort, true
		}
	case "LoadBalancer":
		if _, ok := svc.NodePorts[port]; ok {
			// no mapping needed
			return port, true
		}
	case "ClusterIP":
		if clusterPort, ok := svc.ClusterPorts[port]; ok {
			// return the mapped port
			return clusterPort, true
		}
	}

	return 0, false
}

func (s *servicesVars) Port(serviceName string, port int) string {
	// port and service name given, extract port from service
	if svc, ok := s.services[serviceName]; ok {
		if mappedPort, ok := mapPort(svc, port); ok {
			return strconv.Itoa(mappedPort)
		}
	}

	return ""
}

func (s *servicesVars) FindPort(port int) string {
	// only port given, find any matching service
	for _, svc := range s.services {
		if mappedPort, ok := mapPort(svc, port); ok {
			return strconv.Itoa(mappedPort)
		}
	}

	return ""
}

func (s *servicesVars) IP(serviceName string) string {
	// service name given, extract
	if svc, ok := s.services[serviceName]; ok {
		switch svc.Type {
		case "ClusterIP":
			return svc.ClusterIP
		case "NodePort":
			return extractAddress(s.nodes)
		case "LoadBalancer":
			return svc.ExternalIP
		}
	}

	return ""
}

func (s *servicesVars) FindIP() string {
	// returns first matching service: LoadBalancer > NodePort > ClusterIP
	for _, svc := range s.services {
		if svc.Type == "LoadBalancer" {
			return svc.ExternalIP
		}
	}

	for _, svc := range s.services {
		if svc.Type == "NodePort" {
			return extractAddress(s.nodes)
		}
	}

	for _, svc := range s.services {
		if svc.Type == "ClusterIP" {
			return svc.ClusterIP
		}
	}

	return ""
}

func extractAddress(kubernetesNodes []kubectl.Node) string {
	// return dns name if set as environment variable
	if value, ok := os.LookupEnv("DOMAIN"); ok {
		return value
	}

	for _, node := range kubernetesNodes {
		if len(node.ExternalIP) > 0 {
			return node.ExternalIP
		}
	}

	for _, node := range kubernetesNodes {
		if len(node.InternalIP) > 0 {
			return node.InternalIP
		}
	}

	return ""
}

func extractHostname(kubernetesNodes []kubectl.Node) string {
	for _, node := range kubernetesNodes {
		if len(node.Hostname) > 0 {
			return node.Hostname
		}
	}

	return ""
}

func (s *Service) ReleaseSection(plan *Plan, kubernetesNodes []kubectl.Node, helmStatus helm.Status, values map[string]interface{}) (*Release, error) {
	metadata, err := ExtractMetadata(values)
	if err != nil {
		return nil, err
	}

	env := credentialVars{
		Service: s,
		Plan:    plan,
		Values:  values,
		Release: releaseVars{
			Name:      helmStatus.Name,
			Namespace: helmStatus.Namespace,
		},
		Cluster: &clusterVars{
			Address:       extractAddress(kubernetesNodes),
			Hostname:      extractHostname(kubernetesNodes),
			IngressDomain: metadata.IngressDomain,
		},
		Services: &servicesVars{
			nodes:    kubernetesNodes,
			services: helmStatus.Services,
		},
	}

	b := new(bytes.Buffer)
	err = s.credentialsTemplate.Execute(b, env)
	if err != nil {
		return nil, err
	}

	var section struct {
		UserCredentials map[interface{}]interface{} `yaml:"user-credentials"`
		HealthCheckURLs []string                    `yaml:"health-checks"`
	}
	err = yaml.Unmarshal(b.Bytes(), &section)
	if err != nil {
		return nil, err
	}

	serviceCreds := toStringMap(section.UserCredentials)
	planCreds := toStringMap(plan.UserCredentials)
	credentials := mergeMaps(serviceCreds, planCreds)

	release := &Release{
		UserCredentials: credentials,
		HealthCheckURLs: section.HealthCheckURLs,
	}

	return release, nil
}