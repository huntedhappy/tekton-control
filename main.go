// File: main.go
package main

import (
    "flag"
    "os"

    corev1 "k8s.io/api/core/v1"
    "k8s.io/apimachinery/pkg/runtime"
    utilruntime "k8s.io/apimachinery/pkg/util/runtime"
    clientgoscheme "k8s.io/client-go/kubernetes/scheme"
    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/log/zap"

    pipelinev1beta1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
    "tekton-controller/controllers"
)

var (
    scheme   = runtime.NewScheme()
    setupLog = ctrl.Log.WithName("setup")
)

func init() {
    // 기본 쿠버네티스 타입 등록 (Namespace, Pod, ...)
    utilruntime.Must(clientgoscheme.AddToScheme(scheme))
    // core/v1 Namespace 타입 스킴에 추가
    utilruntime.Must(corev1.AddToScheme(scheme))
    // unstructured 로 Workload, HTTPProxy를 감시하기 때문에 별도 CRD 스킴 추가 불필요

    // Tekton 스킴 등록을 init() 함수로 이동
    utilruntime.Must(pipelinev1beta1.AddToScheme(scheme))
}

func main() {
    var metricsAddr string
    var enableLeaderElection bool

    flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
    flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager.")
    flag.Parse()

    ctrl.SetLogger(zap.New(zap.UseDevMode(true)))


    mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
        Scheme:                 scheme,
        LeaderElection:         enableLeaderElection,
        LeaderElectionID:       "tekton-controller-lock",
    })

    if err != nil {
        setupLog.Error(err, "unable to start manager")
        os.Exit(1)
    }

    // 기존 WorkloadReconciler (unstructured)
    if err = (&controllers.WorkloadReconciler{
        Client: mgr.GetClient(),
        Scheme: mgr.GetScheme(),
    }).SetupWithManager(mgr); err != nil {
        setupLog.Error(err, "unable to create controller", "controller", "Workload")
        os.Exit(1)
    }

    // 네임스페이스 삭제 정리용 Reconciler
    if err = (&controllers.NamespaceCleanupReconciler{
        Client: mgr.GetClient(),
        Scheme: mgr.GetScheme(),
    }).SetupWithManager(mgr); err != nil {
        setupLog.Error(err, "unable to create controller", "controller", "NamespaceCleanup")
        os.Exit(1)
    }

    setupLog.Info("starting manager")
    if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
        setupLog.Error(err, "problem running manager")
        os.Exit(1)
    }
}
