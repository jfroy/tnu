package main

import (
	"context"
	"fmt"
	"log"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/k8s"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/distribution/reference"
	flag "github.com/spf13/pflag"
)

type TalosUpdater struct {
	nodeAddr   string
	nodeName   string
	imageTag   string
	powercycle bool
	client     *client.Client
}

func NewTalosUpdater(nodeAddr, imageTag string, powercycle bool) (*TalosUpdater, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	c, err := client.New(client.WithNode(ctx, nodeAddr), client.WithDefaultConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to create Talos client: %w", err)
	}

	return &TalosUpdater{
		nodeAddr:   nodeAddr,
		imageTag:   imageTag,
		powercycle: powercycle,
		client:     c,
	}, nil
}

func (tu *TalosUpdater) fetchNodename(ctx context.Context) (string, error) {
	r, err := tu.client.COSI.Get(ctx, resource.NewMetadata("k8s", "Nodenames.kubernetes.talos.dev", "node", resource.VersionUndefined))
	if err != nil {
		return "", err
	}
	nn, ok := r.(*k8s.Nodename)
	if !ok {
		return "", fmt.Errorf("unexpected resource type")
	}
	return nn.TypedSpec().Nodename, nil
}

func (tu *TalosUpdater) fetchMachineConfig(ctx context.Context) (*config.MachineConfig, error) {
	r, err := tu.client.COSI.Get(ctx, resource.NewMetadata("config", "MachineConfigs.config.talos.dev", "v1alpha1", resource.VersionUndefined))
	if err != nil {
		return nil, err
	}
	mc, ok := r.(*config.MachineConfig)
	if !ok {
		return nil, fmt.Errorf("unexpected resource type")
	}
	return mc, nil
}

func (tu *TalosUpdater) getSchematicAnnotation(ctx context.Context) (string, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return "", fmt.Errorf("failed to get cluster config: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return "", fmt.Errorf("failed to create Kubernetes client: %w", err)
	}
	node, err := clientset.CoreV1().Nodes().Get(ctx, tu.nodeName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get node %s: %w", tu.nodeName, err)
	}
	v, ok := node.Annotations["extensions.talos.dev/schematic"]
	if !ok {
		return "", fmt.Errorf("schematic annotation not found for node %s", tu.nodeName)
	}
	return v, nil
}

func (tu *TalosUpdater) Update(ctx context.Context) (bool, error) {
	nn, err := tu.fetchNodename(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to fetch nodename resource: %w", err)
	}
	tu.nodeName = nn

	mc, err := tu.fetchMachineConfig(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to fetch machine config: %w", err)
	}

	image := mc.Config().Machine().Install().Image()
	ntref, err := parseReference(image)
	if err != nil {
		return false, fmt.Errorf("failed to parse reference: %w", err)
	}

	vresp, err := tu.client.Version(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to fetch version: %w", err)
	}

	nodeSchematic, err := tu.getSchematicAnnotation(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to get schematic annotation: %w", err)
	}

	if vresp.Messages[0].GetVersion().GetTag() == tu.imageTag && ntref.Name() == nodeSchematic {
		log.Printf("node is up-to-date (schematic: %s, tag: %s)", ntref.Name(), tu.imageTag)
		return false, nil
	}

	ntref, err = reference.WithTag(ntref, tu.imageTag)
	if err != nil {
		return false, fmt.Errorf("failed to update reference tag: %w", err)
	}

	rebootMode := machineapi.UpgradeRequest_DEFAULT
	if tu.powercycle {
		rebootMode = machineapi.UpgradeRequest_POWERCYCLE
	}

	log.Printf("updating %s to %s", tu.nodeName, ntref)
	uresp, err := tu.client.UpgradeWithOptions(ctx,
		client.WithUpgradeImage(ntref.String()),
		client.WithUpgradePreserve(true),
		client.WithUpgradeStage(true),
		client.WithUpgradeRebootMode(rebootMode),
	)
	if err != nil {
		return false, fmt.Errorf("upgrade failed: %w", err)
	}

	log.Printf("update started: %s\n", uresp.GetMessages()[0].String())
	return true, nil
}

func parseReference(image string) (reference.NamedTagged, error) {
	ref, err := reference.ParseAnyReference(image)
	if err != nil {
		return nil, err
	}
	ntref, ok := ref.(reference.NamedTagged)
	if !ok {
		return nil, fmt.Errorf("not a NamedTagged reference")
	}
	return ntref, nil
}

func main() {
	var (
		nodeAddr   string
		imageTag   string
		powercycle bool
	)

	flag.StringVar(&nodeAddr, "node", "", "The address of the node to upgrade (required).")
	flag.StringVar(&imageTag, "tag", "", "The image tag to upgrade to (required).")
	flag.BoolVar(&powercycle, "powercycle", false, "If set, the machine will reboot using powercycle instead of kexec.")
	flag.Usage = func() {
		log.Printf("usage: tnu --node <node> --tag <tag> [--powercycle]\n%s", flag.CommandLine.FlagUsages())
	}
	flag.Parse()

	if nodeAddr == "" || imageTag == "" {
		log.Fatalf("missing required flags: --node and --tag are required\n%s", flag.CommandLine.FlagUsages())
	}

	ctx := context.Background()
	updater, err := NewTalosUpdater(nodeAddr, imageTag, powercycle)
	if err != nil {
		log.Fatalf("failed to initialize updater: %v", err)
	}

	issued, err := updater.Update(ctx)
	if err != nil {
		log.Fatalf("update process failed: %v", err)
	}
	if issued {
		<-make(chan int, 1)
	}
}
