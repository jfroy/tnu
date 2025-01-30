package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/distribution/reference"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/k8s"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	flag "github.com/spf13/pflag"
)

func main() {
	var (
		nodeName   string
		imageTag   string
		rebootMode string
	)
	flag.StringVar(&nodeName, "node", "", "The name of the node to upgrade (required).")
	flag.StringVar(&imageTag, "tag", "", "The image tag to upgrade to (required).")
	flag.StringVar(&rebootMode, "reboot-mode", "default", "Select the reboot mode during upgrade (valid values are: default, powercycle)")
	flag.Usage = func() {
		log.Printf("usage: tnu --node <node> --tag <tag> [--reboot-mode <mode>]\n%s", flag.CommandLine.FlagUsages())
	}
	flag.Parse()

	if nodeName == "" || imageTag == "" {
		log.Fatalf("missing required flags: --node and --tag are required\n%s", flag.CommandLine.FlagUsages())
	}

	rebootModeType, err := parseRebootMode(rebootMode)
	if err != nil {
		log.Fatalf("invalid reboot mode: %s\n%s", rebootMode, err)
	}

	ctx := client.WithNode(context.Background(), nodeName)
	c, err := client.New(ctx, client.WithDefaultConfig())
	if err != nil {
		panic(err)
	}

	r, err := c.COSI.Get(ctx, resource.NewMetadata("k8s", "Nodenames.kubernetes.talos.dev", "nodename", resource.VersionUndefined))
	if err != nil {
		panic(err)
	}
	nn, ok := r.(*k8s.Nodename)
	if !ok {
		panic("not a k8s.Nodename")
	}
	if nn.TypedSpec().Nodename != nodeName {
		log.Fatalf("expected node %s, got %s", nodeName, nn.TypedSpec().Nodename)
	}

	r, err = c.COSI.Get(ctx, resource.NewMetadata("config", "MachineConfigs.config.talos.dev", "v1alpha1", resource.VersionUndefined))
	if err != nil {
		panic(err)
	}
	mc, ok := r.(*config.MachineConfig)
	if !ok {
		panic("not a config.MachineConfig")
	}
	image := mc.Config().Machine().Install().Image()
	ref, err := reference.ParseAnyReference(image)
	if err != nil {
		panic(err)
	}
	ntref, ok := ref.(reference.NamedTagged)
	if !ok {
		panic("not a reference.NamedTagged")
	}

	vresp, err := c.Version(ctx)
	if err != nil {
		panic(err)
	}
	mcSchematic := ntref.Name()[strings.LastIndex(ntref.Name(), "/")+1:]
	nodeSchematic := getSchematicAnnotation(ctx, nodeName)
	if vresp.Messages[0].GetVersion().GetTag() == imageTag && mcSchematic == nodeSchematic {
		log.Printf("node is up-to-date (schematic: %s, tag: %s)", mcSchematic, imageTag)
		os.Exit(0)
	}

	ntref, err = reference.WithTag(ntref, imageTag)
	if err != nil {
		panic(err)
	}

	log.Printf("upgrading %s to %s", nodeName, ntref)
	uresp, err := c.UpgradeWithOptions(ctx,
		client.WithUpgradeImage(ntref.String()),
		client.WithUpgradePreserve(true),
		client.WithUpgradeStage(true),
		client.WithUpgradeRebootMode(rebootModeType),
	)
	if err != nil {
		panic(err)
	}
	fmt.Printf("upgrade started: %s\n", uresp.GetMessages()[0].String())
	<-make(chan int, 1)
}

func getSchematicAnnotation(ctx context.Context, nodename string) string {
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}
	node, err := clientset.CoreV1().Nodes().Get(ctx, nodename, metav1.GetOptions{})
	if err != nil {
		panic(err)
	}
	v, ok := node.Annotations["extensions.talos.dev/schematic"]
	if !ok {
		panic("extensions.talos.dev/schematic annotation not found")
	}
	return v
}

func parseRebootMode(mode string) (machineapi.UpgradeRequest_RebootMode, error) {
	switch mode {
	case "default":
		return machineapi.UpgradeRequest_DEFAULT, nil
	case "powercycle":
		return machineapi.UpgradeRequest_POWERCYCLE, nil
	default:
		return 0, fmt.Errorf("valid values are 'default' or 'powercycle'")
	}
}
