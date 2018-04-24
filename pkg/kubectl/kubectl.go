package kubectl

import (
	"os"
	"path/filepath"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type Node struct {
	Name string

	Hostname   string
	InternalIP string
	ExternalIP string
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
