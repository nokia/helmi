package kubectl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const HelmiSvcDomain = "monostream.com/helmi-svc-domain"

type Node struct {
	Name string

	Hostname   string
	InternalIP string
	ExternalIP string
}

type Namespace struct {
	Name          string
	IngressDomain string
}

type Service struct {
	Type         string
	NodePorts    map[int]int
	ClusterPorts map[int]int
	ExternalIP   string
	ClusterIP    string
}

func createClient() (*kubernetes.Clientset, error) {
	homePath := os.Getenv("HOME")

	if homePath == "" {
		os.Getenv("USERPROFILE")
	}

	configPath := filepath.Join(homePath, ".kube", "config")

	if _, err := os.Stat(configPath); err == nil {
		config, err := clientcmd.BuildConfigFromFlags("", configPath)

		if err != nil {
			return nil, err
		}

		return kubernetes.NewForConfig(config)
	}

	config, err := rest.InClusterConfig()

	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(config)
}

func GetNodes() ([]Node, error) {
	clientset, err := createClient()

	if err != nil {
		return nil, err
	}

	var nodes []Node

	items, err := clientset.CoreV1().Nodes().List(metav1.ListOptions{})

	if err != nil {
		return nil, err
	}

	for _, item := range items.Items {
		node := Node{}

		node.Name = item.Name

		for _, address := range item.Status.Addresses {
			if address.Type == "Hostname" {
				node.Hostname = address.Address
			}

			if address.Type == "InternalIP" {
				node.InternalIP = address.Address
			}

			if address.Type == "ExternalIP" {
				node.ExternalIP = address.Address
			}
		}

		nodes = append(nodes, node)
	}

	return nodes, nil
}

func GetNamespaceByName(name string) (Namespace, error) {
	client, err := createClient()
	if err != nil {
		return Namespace{}, err
	}

	item, err := client.CoreV1().Namespaces().Get(name, metav1.GetOptions{})
	if err != nil {
		return Namespace{}, err
	}

	namespace := Namespace{
		Name:          item.Name,
		IngressDomain: item.Annotations[HelmiSvcDomain],
	}

	return namespace, nil
}

func GetNamespaces(selector map[string]string) ([]Namespace, error) {
	namespaces := make([]Namespace, 0)

	client, err := createClient()

	if err != nil {
		return namespaces, err
	}

	var labels []string
	for k, v := range selector {
		labels = append(labels, fmt.Sprintf("%s=%s", k, v))
	}

	items, err := client.CoreV1().Namespaces().List(metav1.ListOptions{LabelSelector: strings.Join(labels, ",")})

	if err != nil {
		return namespaces, err
	}

	for _, item := range items.Items {
		namespaces = append(namespaces, Namespace{
			Name:          item.Name,
			IngressDomain: item.Annotations[HelmiSvcDomain],
		})
	}

	return namespaces, nil
}

func GetService(name string, ns string) (Service, error) {
	client, err := createClient()

	if err != nil {
		return Service{}, err
	}

	svc, err := client.CoreV1().Services(ns).Get(name, metav1.GetOptions{})

	if err != nil {
		return Service{}, err
	}

	service := Service{
		Type:         string(svc.Spec.Type),
		ClusterIP:    svc.Spec.ClusterIP,
		NodePorts:    make(map[int]int),
		ClusterPorts: make(map[int]int),
	}

	for _, p := range svc.Spec.Ports {
		if p.NodePort != 0 {
			service.NodePorts[int(p.Port)] = int(p.NodePort)
		} else {
			service.ClusterPorts[int(p.Port)] = int(p.Port)
		}
	}

	for _, lb := range svc.Status.LoadBalancer.Ingress {
		if len(lb.Hostname) != 0 {
			service.ExternalIP = lb.Hostname
		} else {
			service.ExternalIP = lb.IP
		}
	}

	return service, nil
}
