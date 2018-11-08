package controller

import (
	"fmt"
	"github.com/golang/glog"
	"gopkg.in/yaml.v2"
	"io/ioutil"

	coreV1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func GetAliases(app string, config []Config) []coreV1.HostAlias {
	for _, conf := range config {
		glog.V(5).Infof("looking for %s using %s", app, conf.Label)
		if conf.Label == app {
			return conf.Aliases
		}
	}
	return nil
}

func GetAliasesByKV(k, v string, config []Config) []coreV1.HostAlias {
	for _, conf := range config {
		glog.V(5).Infof("looking for %s, %s using %s", k, v, conf.Label)
		if conf.App == k && conf.Label == v {
			return conf.Aliases
		}
	}
	return nil
}

func ConfigMapToConfig(cm *coreV1.ConfigMap) (*[]Config, error) {
	var c []Config
	err := yaml.Unmarshal([]byte(cm.Data["config"]), &c)
	if err != nil {
		return nil, err
	}
	glog.V(5).Infof("configs %+v", c)
	return &c, err
}

func FileToConfig(filePath string) (*[]Config, error) {
	var c []Config
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %v", filePath, err)
	}
	err = yaml.Unmarshal(data, &c)
	if err != nil {
		return nil, err
	}
	glog.V(5).Infof("configs %+v", c)
	return &c, err

}

// Get a clientset with in-cluster config.
func GetClient(kubeMaster, kubeConfig string) *kubernetes.Clientset {
	var clusterConfig *rest.Config
	var err error
	if len(kubeMaster) > 0 || len(kubeConfig) > 0 {
		clusterConfig, err = clientcmd.BuildConfigFromFlags(kubeMaster, kubeConfig)
	} else {
		clusterConfig, err = rest.InClusterConfig()
	}

	if err != nil {
		glog.Fatal(err.Error())
	}

	clientset, err := kubernetes.NewForConfig(clusterConfig)
	if err != nil {
		glog.Fatal(err)
	}
	return clientset
}
