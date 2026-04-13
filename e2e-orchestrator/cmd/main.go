/*
Copyright 2024 The Nephio Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	e2ev1alpha1 "e2e.intent.domain/e2e-orchestrator/api/v1alpha1"
	"e2e.intent.domain/e2e-orchestrator/internal/controller"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	// Register e2e-orchestrator CRDs (E2EQoSIntent)
	utilruntime.Must(e2ev1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var porchNamespace string
	var porchPublishedPackage string
	var free5gcURL string
	var free5gcUsername string
	var free5gcPassword string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":9443", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&porchNamespace, "porch-namespace", "default", "Namespace where Porch PackageRevisions are stored.")
	flag.StringVar(&porchPublishedPackage, "porch-published-package", "regional.srsran-gnb.packagevariant-1",
		"The ID of the published srsran-gnb package to use as base.")
	flag.StringVar(&free5gcURL, "free5gc-url", "", "free5GC WebConsole URL (e.g., http://localhost:5000). If empty, UE registration is skipped.")
	flag.StringVar(&free5gcUsername, "free5gc-username", "admin", "free5GC WebConsole username.")
	flag.StringVar(&free5gcPassword, "free5gc-password", "free5gc", "free5GC WebConsole password.")

	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "e2e-orchestrator.e2e.intent.domain",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// Initialize Porch client for RAN domain orchestration
	porchClient := controller.NewPorchClient(porchNamespace, porchPublishedPackage)

	// Initialize free5GC WebConsole client for Core domain UE registration
	var free5gcClient *controller.Free5GCClient
	if free5gcURL != "" {
		free5gcClient = controller.NewFree5GCClient(free5gcURL, free5gcUsername, free5gcPassword)
		setupLog.Info("free5GC WebConsole client configured", "url", free5gcURL)
	} else {
		setupLog.Info("free5GC WebConsole URL not configured, UE registration will be skipped")
	}

	// Set up the E2EQoSIntent controller
	if err = (&controller.E2EQoSIntentReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		PorchClient:   porchClient,
		Free5GCClient: free5gcClient,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "E2EQoSIntent")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting e2e-orchestrator manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
