package main

import (
	"context"
	"fmt"
	"log"
	"strings"

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
	nodeName   string
	imageTag   string
	powercycle bool
	client     *client.Client
}

func NewTalosUpdater(ctx context.Context, imageTag string, powercycle bool) (*TalosUpdater, error) {
	c, err := client.New(ctx, client.WithDefaultConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to create Talos client: %w", err)
	}
	return &TalosUpdater{
		imageTag:   imageTag,
		powercycle: powercycle,
		staged:     staged,
		client:     c,
	}, nil
}

func (tu *TalosUpdater) fetchNodename(ctx context.Context) (string, error) {
	r, err := tu.client.COSI.Get(ctx, resource.NewMetadata("k8s", "Nodenames.kubernetes.talos.dev", "nodename", resource.VersionUndefined))
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
	log.Printf("looking at node %s", tu.nodeName)

	mc, err := tu.fetchMachineConfig(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to fetch machine config: %w", err)
	}

	mcImage := mc.Config().Machine().Install().Image()
	log.Printf("machineconfig install image: %s", mcImage)
	mcImageNTR, err := parseReference(mcImage)
	if err != nil {
		return false, fmt.Errorf("failed to parse machineconfig install image: %w", err)
	}
	mcSchematic := mcImageNTR.Name()[strings.LastIndex(mcImageNTR.Name(), "/")+1:]
	nodeSchematic, err := tu.getSchematicAnnotation(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to get schematic annotation: %w", err)
	}

	vresp, err := tu.client.Version(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to fetch version: %w", err)
	}
	version := vresp.Messages[0].GetVersion()
	log.Printf("talos version: %v", version)

	if version.GetTag() == tu.imageTag && nodeSchematic == mcSchematic {
		log.Printf("node is up-to-date (schematic: %s, tag: %s)", nodeSchematic, version.GetTag())
		return false, nil
	}

	updateImage, err := reference.WithTag(mcImageNTR, tu.imageTag)
	if err != nil {
		return false, fmt.Errorf("failed to update image tag: %w", err)
	}

	rebootMode := machineapi.UpgradeRequest_DEFAULT
	if tu.powercycle {
		rebootMode = machineapi.UpgradeRequest_POWERCYCLE
	}

	log.Printf("updating %s to %v", tu.nodeName, updateImage)
	uresp, err := tu.client.UpgradeWithOptions(ctx,
		client.WithUpgradeImage(updateImage.String()),
		client.WithUpgradePreserve(true),
		client.WithUpgradeStage(tu.staged),
		client.WithUpgradeRebootMode(rebootMode),
	)
	if err != nil {
		return false, fmt.Errorf("update failed: %w", err)
	}

	log.Printf("update started: %s", uresp.GetMessages()[0].String())
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

	flag.StringVar(&nodeAddr, "node", "", "The address of the node to update (required).")
	flag.StringVar(&imageTag, "tag", "", "The image tag to update to (required).")
	flag.BoolVar(&powercycle, "powercycle", false, "If set, the machine will reboot using powercycle instead of kexec.")
	flag.BoolVar(&staged, "staged", false, "Perform the upgrade after a reboot")
	flag.Usage = func() {
		log.Printf("usage: tnu --node <node> --tag <tag> [--powercycle] [--staged]\n%s", flag.CommandLine.FlagUsages())
	}
	flag.Parse()

	if nodeAddr == "" || imageTag == "" {
		log.Fatalf("missing required flags: --node and --tag are required\n%s", flag.CommandLine.FlagUsages())
	}

	ctx := client.WithNode(context.Background(), nodeAddr)
	updater, err := NewTalosUpdater(ctx, imageTag, powercycle, staged)
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
