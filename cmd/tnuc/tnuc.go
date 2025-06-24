package main

import (
	"context"
	"flag"
	"log"
	"os"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type NodeReconciler struct {
	client.Client
}

func (r *NodeReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	var node corev1.Node
	if err := r.Get(ctx, req.NamespacedName, &node); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}
	log.Printf("Node %s changed. Labels: %v, Annotations: %v", node.Name, node.Labels, node.Annotations)
	return reconcile.Result{}, nil
}

func main() {
	var metricsAddr string
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.Parse()

	logger := zap.New(zap.UseDevMode(true))

	cfg, err := config.GetConfig()
	if err != nil {
		logger.Error(err, "failed to get kubeconfig")
		os.Exit(1)
	}
	mgr, err := manager.New(cfg, manager.Options{
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		Logger: logger,
	})
	if err != nil {
		log.Fatalf("failed to start manager: %v", err)
	}

	r := &NodeReconciler{Client: mgr.GetClient()}
	c, err := controller.New("talos-node-update-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		log.Fatalf("failed to create controller: %v", err)
	}

	err = c.Watch(
		source.Kind(mgr.GetCache(), &corev1.Node{}),
		&handler.EnqueueRequestForObject{},
		predicate.Or(predicate.GenerationChangedPredicate{}, predicate.AnnotationChangedPredicate{}))
	if err != nil {
		log.Fatalf("failing watching nodes: %v", err)
	}

	log.Printf("starting manager")
	if err := mgr.Start(context.Background()); err != nil {
		log.Fatalf("failed running manager: %v", err)
	}
}
