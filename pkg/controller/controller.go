package controller

import (
	"encoding/json"
	"os"
	"sync"
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
	PVAnnotation            string
	IntializerConfigmapName string
	InitializerName         string
	IntializerNamespace     string
)

type Config struct {
	Name       string `"yaml: name"`
	Label      string `"yaml: label"`
	Attributes string `"yaml: attributes"`
}

type Controller struct {
	clientset     *kubernetes.Clientset
	podPVCMap     map[string]string
	podPVCLock    *sync.Mutex
	podController cache.Controller
	pvcController cache.Controller
	config        *[]Config
}

func NewPVInitializer(clientset *kubernetes.Clientset, conf *[]Config) *Controller {
	c := &Controller{
		config:     conf,
		clientset:  clientset,
		podPVCMap:  make(map[string]string),
		podPVCLock: &sync.Mutex{},
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

	pvcListWatcher := cache.NewListWatchFromClient(
		restClient,
		"persistentvolumeclaims",
		coreV1.NamespaceAll,
		fields.Everything())

	_, pvcController := cache.NewInformer(
		pvcListWatcher,
		&coreV1.PersistentVolumeClaim{},
		resyncPeriod,
		cache.ResourceEventHandlerFuncs{
			UpdateFunc: func(old, new interface{}) {
				err := c.updatePVC(old.(*coreV1.PersistentVolumeClaim), new.(*coreV1.PersistentVolumeClaim))
				if err != nil {
					glog.Warningf("failed to initialized: %v", err)
					return
				}
			},
		},
	)
	c.pvcController = pvcController
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
	glog.Infof("pvc controller starting")
	go c.pvcController.Run(ctx)
	glog.Infof("Waiting for pvc informer initial sync")
	wait.Poll(time.Second, 5*time.Minute, func() (bool, error) {
		return c.pvcController.HasSynced(), nil
	})
	if !c.pvcController.HasSynced() {
		glog.Errorf("pvc informer controller initial sync timeout")
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
					attr := c.getAttributes(app)
					if len(attr) > 0 {
						vols := pod.Spec.Volumes
						for _, vol := range vols {
							if vol.VolumeSource.PersistentVolumeClaim != nil {
								pvcName := vol.VolumeSource.PersistentVolumeClaim.ClaimName
								glog.V(3).Infof("PVC %s", pvcName)
								pvc, err := c.clientset.CoreV1().PersistentVolumeClaims(pod.Namespace).Get(pvcName, metaV1.GetOptions{})
								if err == nil {
									// if PVC is bound, update PV.
									pvName := pvc.Spec.VolumeName
									if len(pvName) > 0 {
										c.updatePVAnnotation(pvName, attr)
									} else {
										// defer till PVC is bound
										c.updatePodPVCMap(pod.Namespace, pvcName, attr, true /* toAdd */)
									}
								}
							}
						}
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

func (c *Controller) updatePVAnnotation(pvName, data string) {
	pv, err := c.clientset.CoreV1().PersistentVolumes().Get(pvName, metaV1.GetOptions{})
	if err == nil {
		glog.V(3).Infof("update PV %s", pv.Name)
		ann := pv.ObjectMeta.GetAnnotations()
		if ann == nil {
			var m map[string]string
			m[PVAnnotation] = data
			pv.ObjectMeta.SetAnnotations(m)
		} else {
			existingAnn := ann[PVAnnotation]
			if len(existingAnn) == 0 {
				// annotation doesn't exist, just add
				ann[PVAnnotation] = data
			} else {
				// append to existing annotation
				attrs := map[string]interface{}{}
				existingAttrs := map[string]interface{}{}
				glog.V(5).Infof("updating %s with %s", existingAnn, data)
				err1 := json.Unmarshal([]byte(data), &attrs)
				err2 := json.Unmarshal([]byte(existingAnn), &existingAttrs)
				if err1 == nil && err2 == nil {
					for k, v := range attrs {
						glog.V(5).Infof("add %v %v", k, v)
						existingAttrs[k] = v
					}
					newAnn, err := json.Marshal(existingAttrs)
					if err == nil {
						ann[PVAnnotation] = string(newAnn)
					}
				}
			}
			glog.V(3).Infof("updating with new annotation %+v", ann)
			pv.ObjectMeta.SetAnnotations(ann)
		}
		_, err := c.clientset.CoreV1().PersistentVolumes().Update(pv)
		if err != nil {
			glog.Warningf("failed to update pv :%v", err)
		}
	}
}

func (c *Controller) updatePVC(oldPVC, newPVC *coreV1.PersistentVolumeClaim) error {
	ns := newPVC.Namespace
	name := newPVC.Name

	if data := c.getPodPVCMap(ns, name); len(data) > 0 {
		// if pvc is bound and pv exists, update pv annotation
		if newPVC.Status.Phase == coreV1.ClaimBound {
			pvName := newPVC.Spec.VolumeName
			if len(pvName) > 0 {
				c.updatePVAnnotation(pvName, data)
				c.updatePodPVCMap(ns, name, "", false /* toAdd */)
			}
		}
	}

	return nil
}

func (c *Controller) updatePodPVCMap(pvcNS, pvcName, attr string, toAdd bool) {
	c.podPVCLock.Lock()
	defer c.podPVCLock.Unlock()
	key := pvcNS + "/" + pvcName
	glog.V(5).Infof("updating map: %s/%s with %s %v", pvcNS, pvcName, attr, toAdd)
	if toAdd {
		c.podPVCMap[key] = attr
	} else {
		delete(c.podPVCMap, key)
	}
}

func (c *Controller) getPodPVCMap(pvcNS, pvcName string) string {
	c.podPVCLock.Lock()
	defer c.podPVCLock.Unlock()
	key := pvcNS + "/" + pvcName
	glog.V(5).Infof("get map: %s/%s", pvcNS, pvcName)
	val, ok := c.podPVCMap[key]
	if ok {
		return val
	}
	return ""
}

func (c *Controller) getAttributes(app string) string {
	for _, conf := range *c.config {
		if conf.Label == app {
			return conf.Attributes
		}
	}
	return ""
}
