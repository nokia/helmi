package catalog

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig"
	"github.com/satori/go.uuid"

	"github.com/monostream/helmi/pkg/helm"
	"github.com/monostream/helmi/pkg/kubectl"
)

type Catalog struct {
	Services map[string]Service
}

type Service struct {
	Id          string `yaml:"_id"`
	Name        string `yaml:"_name"`
	Description string `yaml:"description"`

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

	Chart        string                      `yaml:"chart"`
	ChartVersion string                      `yaml:"chart-version"`
	ChartValues  map[interface{}]interface{} `yaml:"chart-values"`

	UserCredentials map[interface{}]interface{} `yaml:"user-credentials"`
}

// Parses any catalog format: local directories, local zip archives or zip archive urls
func Parse(name string) (Catalog, error) {
	if strings.HasPrefix(name, "http://") || strings.HasPrefix(name, "https://") {
		return ParseZipURL(name)
	}

	fi, err := os.Stat(name)
	if err != nil {
		return Catalog{}, err
	}

	if fi.IsDir() {
		return ParseDir(name)
	} else {
		return ParseZipFile(name)
	}
}

// Parses all `.yaml` and `.yml` files in the specified path as service definitions
func ParseDir(dir string) (Catalog, error) {
	c := Catalog{
		Services: make(map[string]Service),
	}

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

		return c.parseServiceDefinition(input, path)
	})

	if err != nil {
		return c, err
	}

	if len(c.Services) == 0 {
		err = fmt.Errorf("no services found in catalog directory: %s", dir)
		return c, err
	}

	return c, nil
}

func ParseZipFile(file string) (Catalog, error) {
	zipFile, err := zip.OpenReader(file)
	if err != nil {
		return Catalog{}, err
	}
	defer zipFile.Close()

	return parseZipReader(&zipFile.Reader, file)
}

func ParseZipURL(url string) (Catalog, error) {
	resp, err := http.Get(url)
	if err != nil {
		return Catalog{}, err
	}
	defer resp.Body.Close()

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return Catalog{}, err
	}

	reader := bytes.NewReader(b)
	zipReader, err := zip.NewReader(reader, reader.Size())
	if err != nil {
		return Catalog{}, err
	}

	return parseZipReader(zipReader, url)
}

func parseZipReader(zipReader *zip.Reader, path string) (Catalog, error) {
	c := Catalog{
		Services: make(map[string]Service),
	}

	for _, entry := range zipReader.File {
		ext := filepath.Ext(entry.Name)
		if ext != ".yml" && ext != ".yaml" {
			continue
		}

		f, err := entry.Open()
		if err != nil {
			return c, err
		}

		content, err := ioutil.ReadAll(f)
		f.Close()

		if err != nil {
			return c, err
		}

		err = c.parseServiceDefinition(content, path)
		if err != nil {
			return c, err
		}
	}

	if len(c.Services) == 0 {
		err := fmt.Errorf("no services found in catalog zip file: %s", path)
		return c, err
	}

	return c, nil
}

func (c *Catalog) parseServiceDefinition(input []byte, file string) error {
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

	c.Services[s.Id] = s.Service
	return nil
}

func (c *Catalog) Service(id string) *Service {
	if val, ok := c.Services[id]; ok {
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

	randomUuid := func() string {
		s := uuid.NewV4().String()
		s = strings.Replace(s, "-", "", -1)
		return s
	}
	f["generateUsername"] = randomUuid
	f["generatePassword"] = randomUuid

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
	*Service
	*Plan
}

func (s *Service) ChartValues(p *Plan) (map[string]interface{}, error) {
	b := new(bytes.Buffer)
	data := chartValueVars{s, p}
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

	serviceValues := toStringMap(v.ChartValues)
	planValues := toStringMap(p.ChartValues)
	res := mergeMaps(serviceValues, planValues)

	return res, nil
}

type credentialVars struct {
	Service  *Service
	Plan     *Plan
	Values   valueVars
	Release  releaseVars
	Cluster  clusterVars
	Services *servicesVars
}

type valueVars map[string]interface{}

type releaseVars struct {
	Name      string
	Namespace string
}

type clusterVars struct {
	Address  string
	Hostname string
}

type servicesVars struct {
	services map[string]*helm.Service
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

func (s *servicesVars) Port(serviceName string, port int) string {
	// port and service name given, extract port from service
	if svc, ok := s.services[serviceName]; ok {
		if mappedPort, ok := svc.PortMapping(port); ok {
			return strconv.Itoa(mappedPort)
		}
	}

	return ""
}

func (s *servicesVars) FindPort(port int) string {
	// only port given, find any matching service
	for _, svc := range s.services {
		if mappedPort, ok := svc.PortMapping(port); ok {
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

func (s *Service) UserCredentials(plan *Plan, kubernetesNodes []kubectl.Node, helmStatus helm.Status, values map[string]interface{}) (map[string]interface{}, error) {
	env := credentialVars{
		Service: s,
		Plan:    plan,
		Values:  values,
		Release: releaseVars{
			Name:      helmStatus.Name,
			Namespace: helmStatus.Namespace,
		},
		Cluster: clusterVars{
			Address:  extractAddress(kubernetesNodes),
			Hostname: extractHostname(kubernetesNodes),
		},
		Services: &servicesVars{
			nodes:    kubernetesNodes,
			services: helmStatus.Services,
		},
	}

	b := new(bytes.Buffer)
	err := s.credentialsTemplate.Execute(b, env)
	if err != nil {
		return nil, err
	}

	var v struct {
		UserCredentials map[interface{}]interface{} `yaml:"user-credentials"`
	}
	err = yaml.Unmarshal(b.Bytes(), &v)
	if err != nil {
		return nil, err
	}

	serviceCreds := toStringMap(v.UserCredentials)
	planCreds := toStringMap(plan.UserCredentials)
	return mergeMaps(serviceCreds, planCreds), nil
}
