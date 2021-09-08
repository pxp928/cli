package chains

import (
	"fmt"
	"strings"
	"time"

	toto "github.com/in-toto/in-toto-golang/in_toto"
	"github.com/spf13/cobra"
	"github.com/tektoncd/cli/pkg/cli"
	"github.com/tektoncd/cli/pkg/flags"
	"github.com/tektoncd/cli/pkg/pipeline"
	"github.com/tektoncd/cli/pkg/task"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var cliClient *cli.Clients

func getClient(p cli.Params) *cli.Clients {
	// Create the Kubernetes client.
	if cliClient != nil {
		return cliClient
	} else {
		cliClient, _ = p.Clients()
		return cliClient
	}
}

// Create layout for specific taskrun.
func createIntotoLayout(p cli.Params, pipelineName string, path string) (string, error) {
	cs := getClient(p)
	// Get the current time for layout file creation
	currenttime := time.Now()
	currenttime = currenttime.Add(30 * 24 * time.Hour)

	// Describe one taskrun.
	pipeline, err := pipeline.Get(cs, pipelineName, metav1.GetOptions{}, p.Namespace())
	if err != nil {
		return "", fmt.Errorf("failed to get pipeline %s: %v", pipelineName, err)
	}

	var keys = make(map[string]toto.Key)

	intotoSteps, err := getIntotoSteps(p, *pipeline)
	if err != nil {
		return "", fmt.Errorf("failed to get task in pipeline %s: %v", pipelineName, err)
	}
	var metablock = toto.Metablock{
		Signed: toto.Layout{
			Type:    "layout",
			Expires: currenttime.Format("2006-01-02T15:04:05Z"),
			Steps:   intotoSteps,
			Inspect: getIntotoInspect(*pipeline),
			Keys:    keys,
		},
	}

	layoutPath := path + "/root.layout"
	metablock.Dump(layoutPath)

	return layoutPath, nil
}

// Get the task from a pipeline append together to be passed into intoto layout
func getIntotoSteps(p cli.Params, pipeline v1beta1.Pipeline) ([]toto.Step, error) {
	cs := getClient(p)
	var intotoSteps []toto.Step

	for _, pipelinetask := range pipeline.Spec.Tasks {
		task, err := task.Get(cs, pipelinetask.TaskRef.Name, metav1.GetOptions{}, p.Namespace())
		if err != nil {
			return nil, fmt.Errorf("failed to get task %s: %v", pipelinetask.TaskRef.Name, err)
		}
		for _, step := range task.Spec.Steps {

			intotoStep := toto.Step{
				Type:            "step",
				ExpectedCommand: getExpectedCommand(step),
				SupplyChainItem: toto.SupplyChainItem{
					Name:              task.Name,
					ExpectedMaterials: getExpectedMaterials(pipelinetask, false),
					ExpectedProducts:  getExpectedProducts(pipelinetask),
				},
			}
			intotoSteps = append(intotoSteps, intotoStep)
		}
	}
	return intotoSteps, nil
}

// Get the task from a pipeline append together to be passed into intoto layout
func getIntotoInspect(pipeline v1beta1.Pipeline) []toto.Inspection {
	var intotoInsepcts []toto.Inspection
	for _, task := range pipeline.Spec.Tasks {

		intotoInspect := toto.Inspection{
			Type: "Inspect",
			Run:  []string{""},
			SupplyChainItem: toto.SupplyChainItem{
				Name:              task.Name,
				ExpectedMaterials: getExpectedMaterials(task, true),
				//ExpectedProducts:  getExpectedProducts(task),
			},
		}

		intotoInsepcts = append(intotoInsepcts, intotoInspect)
	}

	return intotoInsepcts
}

// Get the commands and arguments from a pipeline step and append together to be passed into intoto layout
func getExpectedCommand(step v1beta1.Step) []string {
	var combined []string

	// combine the command and arguments into one slice
	combined = append(combined, step.Command...)
	combined = append(combined, step.Args...)

	return combined
}

// Get the materials from a pipeline task and append together to be passed into intoto layout
func getExpectedMaterials(task v1beta1.PipelineTask, inspection bool) [][]string {
	var expMats [][]string

	// If not part of the inspection step within the intoto layout grab the matierlas from the input
	if !inspection {
		// Get each input to be placed into intoto layout file
		for _, input := range task.Resources.Inputs {

			// If input comes from a different step in the task, append this information
			if len(input.From) > 0 {
				expMats = append(expMats, [][]string{{"MATCH", input.Resource,
					"WITH", "PRODUCTS", "FROM", strings.Join(input.From, " ")}}...)
			} else {
				expMats = append(expMats, [][]string{{"ALLOW", input.Resource}}...)
			}
		}

		// Append disallow statement to the end of the expected materials
		expMats = append(expMats, [][]string{{"DISALLOW", "*"}}...)

		return expMats

		// If  part of the inspection step within the intoto layout grab the matierlas from the output with the task name
	} else {
		// Get each output to be placed into intoto layout file for inspection
		for _, output := range task.Resources.Outputs {

			expMats = append(expMats, [][]string{{"MATCH", output.Resource,
				"WITH", "PRODUCTS", "FROM", task.Name}}...)

		}

		return expMats
	}

}

// Get the prodcuts from a pipeline task and append together to be passed into intoto layout
func getExpectedProducts(task v1beta1.PipelineTask) [][]string {
	var expProds [][]string

	// Get each ouput to be placed into intoto layout file
	for _, output := range task.Resources.Outputs {
		expProds = append(expProds, [][]string{{"ALLOW", output.Resource}}...)
	}
	// Append disallow statement to the end of the expected prodcuts
	//expProds = append(expProds, [][]string{{"DISALLOW", "*"}}...)

	return expProds
}

func Command(p cli.Params) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "chains",
		Aliases: []string{},
		Short:   "Manage Tekton Chains",
		Annotations: map[string]string{
			"commandType": "main",
		},
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return flags.InitParams(p, cmd)
		},
	}

	flags.AddTektonOptions(cmd)
	_ = cmd.PersistentFlags().MarkHidden("context")
	_ = cmd.PersistentFlags().MarkHidden("kubeconfig")
	_ = cmd.PersistentFlags().MarkHidden("namespace")
	cmd.AddCommand(
		layoutCommand(p),
	)
	return cmd
}
