package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/golang/glog"

	"github.com/fast-ml/nezha/pkg/controller"
	"k8s.io/api/admission/v1beta1"
	extensions "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

var (
	configFile    string
	useTLS        *bool
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
	hostAliasConf *[]controller.Config
	// (https://github.com/kubernetes/kubernetes/issues/57982)
	defaulter                  = runtime.ObjectDefaulter(runtimeScheme)
	addHostAliasesPatch string = `[{"op": "add", "path": "/spec/template/spec/hostAliases", "value": %s }]`
)

// Config contains the server (the webhook) cert and key.
type certConfig struct {
	CertFile string
	KeyFile  string
}

func (c *certConfig) addFlags() {
	flag.StringVar(&configFile, "config-file", "", "path to hostAliases configuration config file")
	flag.StringVar(&c.CertFile, "tls-cert-file", c.CertFile, ""+
		"File containing the default x509 Certificate for HTTPS. (CA cert, if any, concatenated "+
		"after server cert).")
	flag.StringVar(&c.KeyFile, "tls-private-key-file", c.KeyFile, ""+
		"File containing the default x509 private key matching --tls-cert-file.")
}

func toAdmissionResponse(err error) *v1beta1.AdmissionResponse {
	return &v1beta1.AdmissionResponse{
		Result: &metav1.Status{
			Message: err.Error(),
		},
	}
}

func configTLS(config certConfig) *tls.Config {
	sCert, err := tls.LoadX509KeyPair(config.CertFile, config.KeyFile)
	if err != nil {
		glog.Fatal(err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{sCert},
		// TODO: uses mutual tls after we agree on what cert the apiserver should use.
		// ClientAuth:   tls.RequireAndVerifyClientCert,
	}
}

func mutateDeployments(ar v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
	glog.V(2).Info("mutating deployments")
	dpResource := metav1.GroupVersionResource{Group: "extensions", Version: "v1beta1", Resource: "deployments"}
	if ar.Request.Resource != dpResource {
		glog.Errorf("expect resource to be %s", dpResource)
		return nil
	}

	raw := ar.Request.Object.Raw
	dp := extensions.Deployment{}
	deserializer := codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(raw, nil, &dp); err != nil {
		glog.Error(err)
		return toAdmissionResponse(err)
	}
	reviewResponse := v1beta1.AdmissionResponse{}
	reviewResponse.Allowed = true
	if labels := dp.ObjectMeta.GetLabels(); len(labels) > 0 {
		glog.V(5).Infof("labels %v", labels)
		app, ok := labels["app"]
		if ok {
			aliases := controller.GetAliases(app, *hostAliasConf)
			if len(aliases) > 0 {
				spec := dp.Spec.Template.Spec
				if len(spec.HostAliases) > 0 {
					aliases = append(spec.HostAliases, aliases...)
				}
				glog.V(5).Infof("app %v, hosts %v", app, aliases)
				js, err := json.Marshal(aliases)
				if err == nil {
					patch := fmt.Sprintf(addHostAliasesPatch, js)
					glog.V(5).Infof("patch %s", patch)
					reviewResponse.Patch = []byte(patch)
					pt := v1beta1.PatchTypeJSONPatch
					reviewResponse.PatchType = &pt
				}
			}
		}
	}
	return &reviewResponse
}

type admitFunc func(v1beta1.AdmissionReview) *v1beta1.AdmissionResponse

func serveMutateDeployments(w http.ResponseWriter, r *http.Request) {
	serve(w, r, mutateDeployments)
}

func serve(w http.ResponseWriter, r *http.Request, admit admitFunc) {
	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}

	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		glog.Errorf("contentType=%s, expect application/json", contentType)
		return
	}

	glog.V(2).Info(fmt.Sprintf("handling request: %s", string(body)))
	var reviewResponse *v1beta1.AdmissionResponse
	ar := v1beta1.AdmissionReview{}
	deserializer := codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
		glog.Error(err)
		reviewResponse = toAdmissionResponse(err)
	} else {
		reviewResponse = admit(ar)
	}
	glog.V(2).Info(fmt.Sprintf("sending response: %v", reviewResponse))

	response := v1beta1.AdmissionReview{}
	if reviewResponse != nil {
		response.Response = reviewResponse
		response.Response.UID = ar.Request.UID
	}
	// reset the Object and OldObject, they are not needed in a response.
	ar.Request.Object = runtime.RawExtension{}
	ar.Request.OldObject = runtime.RawExtension{}

	resp, err := json.Marshal(response)
	if err != nil {
		glog.Error(err)
	}
	if _, err := w.Write(resp); err != nil {
		glog.Error(err)
	}
}

func main() {
	var certConfig certConfig
	var err error
	certConfig.addFlags()
	flag.Parse()
	flag.Set("logtostderr", "true")
	if len(configFile) == 0 {
		glog.Fatalf("hostAliases config file is empty")
	}
	hostAliasConf, err = controller.FileToConfig(configFile)
	if err != nil {
		glog.Fatalf("failed to parse config file: %v", err)
	}

	tickChan := time.NewTicker(time.Second * 10).C
	go func() {
		for {
			select {
			case <-tickChan:
				newConfig, err := controller.FileToConfig(configFile)
				if err == nil {
					hostAliasConf = newConfig
				} else {
					glog.Warningf("invalid config: %v", err)
				}
			}
		}
	}()

	http.HandleFunc("/mutate", serveMutateDeployments)
	server := &http.Server{
		Addr:      ":443",
		TLSConfig: configTLS(certConfig),
	}
	glog.Infof("starting server")
	server.ListenAndServeTLS("", "")
}
