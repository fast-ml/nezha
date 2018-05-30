package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/golang/glog"
	"gopkg.in/yaml.v2"

	"github.com/fast-ml/nezha/pkg/controller"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	defaultPVAnnotation       = "csi.volume.kubernetes.io/volume-attributes"
	defaultInitializerName    = "pv.initializer.kubernetes.io"
	defaultConfigmapName      = "pv-initializer"
	defaultConfigMapNamespace = "default"
)

var (
	kubeConfig string
	kubeMaster string
)

func main() {
	flag.StringVar(&controller.PVAnnotation, "pv-annotation", defaultPVAnnotation, "PersistentVolume Annotation to patch")
	flag.StringVar(&controller.IntializerConfigmapName, "configmap", defaultConfigmapName, "storage initializer configuration configmap")
	flag.StringVar(&controller.InitializerName, "initializer-name", defaultInitializerName, "The initializer name")
	flag.StringVar(&controller.IntializerNamespace, "namespace", defaultConfigMapNamespace, "The configuration namespace")
	flag.StringVar(&kubeConfig, "kubeconfig", "", "Absolute path to the kubeconfig")
	flag.StringVar(&kubeMaster, "kubemaster", "", "Kubernetes Controller Master URL")
	flag.Parse()
	flag.Set("logtostderr", "true")

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

	cm, err := clientset.CoreV1().ConfigMaps(controller.IntializerNamespace).Get(controller.IntializerConfigmapName, metaV1.GetOptions{})
	if err != nil {
		glog.Fatal(err)
	}
	conf, err := configMapToConfig(cm)
	if err != nil {
		glog.Fatalf("failed to parse configmap: %v", err)
	}
	ctrl := controller.NewPVInitializer(clientset, conf)
	if ctrl == nil {
		glog.Fatal("failed to create initializer")
	}
	glog.Infof("Starting initializer ")
	stop := make(chan struct{})
	go ctrl.Run(stop)

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	<-signalChan

	close(stop)
}

func configMapToConfig(cm *coreV1.ConfigMap) (*[]controller.Config, error) {
	var c []controller.Config
	err := yaml.Unmarshal([]byte(cm.Data["config"]), &c)
	if err != nil {
		return nil, err
	}
	glog.V(5).Infof("configs %+v", c)
	return &c, err
}
