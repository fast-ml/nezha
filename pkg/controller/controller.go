package controller

import (
	"os"
	"time"

	"github.com/golang/glog"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

var (
	IntializerConfigmapName string
	InitializerName         string
	IntializerNamespace     string
)

type Config struct {
	Name    string             `yaml:"name"`
	Label   string             `yaml:"label"`
	Aliases []coreV1.HostAlias `yaml:"hostAliases"`
}

type Controller struct {
	clientset     *kubernetes.Clientset
	podController cache.Controller
	config        *[]Config
}

func NewHostAliasesInitializer(clientset *kubernetes.Clientset, conf *[]Config) *Controller {
	c := &Controller{
		config:    conf,
		clientset: clientset,
	}

	restClient := clientset.CoreV1().RESTClient()
	watchlist := cache.NewListWatchFromClient(restClient, "pods", coreV1.NamespaceAll, fields.Everything())

	// Wrap the returned watchlist to workaround the inability to include
	// the `IncludeUninitialized` list option when setting up watch clients.
	includeUninitializedWatchlist := &cache.ListWatch{
		ListFunc: func(options metaV1.ListOptions) (runtime.Object, error) {
			options.IncludeUninitialized = true
			return watchlist.List(options)
		},
		WatchFunc: func(options metaV1.ListOptions) (watch.Interface, error) {
			options.IncludeUninitialized = true
			return watchlist.Watch(options)
		},
	}

	resyncPeriod := 30 * time.Second

	_, podController := cache.NewInformer(
		includeUninitializedWatchlist,
		&coreV1.Pod{},
		resyncPeriod,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				err := c.addPod(obj.(*coreV1.Pod))
				if err != nil {
					glog.Warningf("failed to initialized: %v", err)
					return
				}
			},
		},
	)
	c.podController = podController

	return c
}

func (c *Controller) Run(ctx <-chan struct{}) {
	glog.Infof("pod controller starting")
	go c.podController.Run(ctx)
	glog.Infof("Waiting for pod informer initial sync")
	wait.Poll(time.Second, 5*time.Minute, func() (bool, error) {
		return c.podController.HasSynced(), nil
	})
	if !c.podController.HasSynced() {
		glog.Errorf("pod informer controller initial sync timeout")
		os.Exit(1)
	}
}

func (c *Controller) addPod(pod *coreV1.Pod) error {
	if pod != nil && pod.ObjectMeta.GetInitializers() != nil {
		pendingInitializers := pod.ObjectMeta.GetInitializers().Pending

		if InitializerName == pendingInitializers[0].Name {
			glog.V(3).Infof("Initializing: %s", pod.Name)

			initializedPod := pod.DeepCopy()
			// Remove self from the list of pending Initializers while preserving ordering.
			if len(pendingInitializers) == 1 {
				initializedPod.ObjectMeta.Initializers = nil
			} else {
				initializedPod.ObjectMeta.Initializers.Pending = append(pendingInitializers[:0], pendingInitializers[1:]...)

			}
			if labels := initializedPod.ObjectMeta.GetLabels(); len(labels) > 0 {
				glog.V(5).Infof("labels %+v", labels)
				app, ok := labels["app"]
				if ok {
					aliases := GetAliases(app, *c.config)
					if len(aliases) > 0 {
						pod.Spec.HostAliases = append(pod.Spec.HostAliases, aliases...)
					}
				}
			}
			_, err := c.clientset.CoreV1().Pods(pod.Namespace).Update(initializedPod)
			if err != nil {
				glog.Warning("failed to update pod %s/%s: %v", pod.Namespace, pod.Name, err)
				return err
			}
			glog.V(3).Infof("Initialized: %s", pod.Name)
		}
	}

	return nil
}
