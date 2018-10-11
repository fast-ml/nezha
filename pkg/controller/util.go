package controller

import (
	"github.com/golang/glog"
	"gopkg.in/yaml.v2"

	coreV1 "k8s.io/api/core/v1"
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

func ConfigMapToConfig(cm *coreV1.ConfigMap) (*[]Config, error) {
	var c []Config
	err := yaml.Unmarshal([]byte(cm.Data["config"]), &c)
	if err != nil {
		return nil, err
	}
	glog.V(5).Infof("configs %+v", c)
	return &c, err
}
