// File: main.go
package main

import (
	"flag"
	"os"
	"path/filepath" // path/filepath 패키지 추가

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	tektonv1alpha1 "tekton-controller/api/v1alpha1"
	"tekton-controller/controllers"
	"tekton-controller/pkg/namespace"

	pipelinev1beta1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(tektonv1alpha1.AddToScheme(scheme))
	utilruntime.Must(pipelinev1beta1.AddToScheme(scheme)) // <-- 추가
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var tlsCertDir string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", false, "If set, the metrics endpoint is served securely.")
	flag.StringVar(&tlsCertDir, "tls-cert-dir", "", "Directory containing the TLS certificate and key. Required if --metrics-secure is set.")

	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// --- 로직 추가: Secure Metrics가 활성화된 경우, 인증서 파일 존재 여부 확인 ---
	if secureMetrics {
		if tlsCertDir == "" {
			setupLog.Error(nil, "--tls-cert-dir must be set if --metrics-secure is true")
			os.Exit(1)
		}
		// 인증서와 키 파일 경로 확인
		certPath := filepath.Join(tlsCertDir, "tls.crt")
		keyPath := filepath.Join(tlsCertDir, "tls.key")
		if _, err := os.Stat(certPath); os.IsNotExist(err) {
			setupLog.Error(err, "tls.crt not found in cert directory", "path", certPath)
			os.Exit(1)
		}
		if _, err := os.Stat(keyPath); os.IsNotExist(err) {
			setupLog.Error(err, "tls.key not found in cert directory", "path", keyPath)
			os.Exit(1)
		}
		setupLog.Info("Secure metrics enabled and TLS certificates found, serving metrics over HTTPS")
	} else {
		setupLog.Info("Secure metrics disabled, serving metrics over HTTP")
	}
	// --- 로직 추가 끝 ---

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: server.Options{
			BindAddress:   metricsAddr,
			SecureServing: secureMetrics, // 이 플래그 값에 따라 HTTP/HTTPS가 결정됨
			CertDir:       tlsCertDir,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "tekton-controller.tekton.platform",
	})

	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// ... 나머지 컨트롤러 설정 ...
	if err = (&controllers.WorkloadReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Workload")
		os.Exit(1)
	}

	if err = (&namespace.NamespaceCleanupReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "NamespaceCleanup")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
