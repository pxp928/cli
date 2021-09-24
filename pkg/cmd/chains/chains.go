package chains

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	toto "github.com/in-toto/in-toto-golang/in_toto"
	"github.com/spf13/cobra"
	"github.com/tektoncd/cli/pkg/cli"
	"github.com/tektoncd/cli/pkg/flags"
	"github.com/tektoncd/cli/pkg/pipelinerun"
	"github.com/tektoncd/cli/pkg/taskrun"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1alpha1"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	commitParam        = "CHAINS-GIT_COMMIT"
	urlParam           = "CHAINS-GIT_URL"
	ociDigestResult    = "IMAGE_DIGEST"
	chainsDigestSuffix = "_DIGEST"
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
func createIntotoLayout(p cli.Params, pipelineRunName string, path string) (string, error) {
	cs := getClient(p)
	// Get the current time for layout file creation
	currenttime := time.Now()
	currenttime = currenttime.Add(30 * 24 * time.Hour)

	// Describe one taskrun.
	pr, err := pipelinerun.Get(cs, pipelineRunName, metav1.GetOptions{}, p.Namespace())
	if err != nil {
		return "", fmt.Errorf("failed to get pipelinerun %s: %v", pipelineRunName, err)
	}

	var keys = make(map[string]toto.Key)

	intotoSteps, err := getIntotoSteps(p, *pr)
	if err != nil {
		return "", fmt.Errorf("failed to get task in pipeline %s: %v", pipelineRunName, err)
	}
	intotoInspect, err := getIntotoInspect(p, *pr)
	if err != nil {
		return "", fmt.Errorf("failed to get inspections steps in pipeline %s: %v", pipelineRunName, err)
	}
	var metablock = toto.Metablock{
		Signed: toto.Layout{
			Type:    "layout",
			Expires: currenttime.Format("2006-01-02T15:04:05Z"),
			Steps:   intotoSteps,
			Inspect: intotoInspect,
			Keys:    keys,
		},
	}

	layoutPath := path + "/root.layout"
	metablock.Dump(layoutPath)

	return layoutPath, nil
}

func container(stepState v1beta1.StepState, tr *v1beta1.TaskRun) v1beta1.Step {
	name := stepState.Name
	if tr.Status.TaskSpec != nil {
		for _, s := range tr.Status.TaskSpec.Steps {
			if s.Name == name {
				return s
			}
		}
	}

	return v1beta1.Step{}
}

// Get the task from a pipeline append together to be passed into intoto layout
func getIntotoSteps(p cli.Params, pr v1beta1.PipelineRun) ([]toto.Step, error) {
	cs := getClient(p)
	var intotoSteps []toto.Step
	var expMats [][]string
	for taskrunName, _ := range pr.Status.TaskRuns {

		tr, err := taskrun.Get(cs, taskrunName, metav1.GetOptions{}, p.Namespace())
		if err != nil {
			return nil, fmt.Errorf("failed to get tr %s: %v", taskrunName, err)
		}

		gitCommit, gitURL := gitInfo(tr)

		// Store git rev as Materials and Recipe.Material
		if gitCommit != "" && gitURL != "" {
			expMats = append(expMats, [][]string{{"ALLOW", gitURL}}...)
		}

		for _, step := range tr.Status.Steps {

			intotoStep := toto.Step{
				Type:            "step",
				ExpectedCommand: getExpectedCommand(container(step, tr)),
				SupplyChainItem: toto.SupplyChainItem{
					Name:              step.Name,
					ExpectedMaterials: append(expMats, getExpectedMaterials(step)...),
					ExpectedProducts:  getExpectedProducts(tr, false),
				},
			}
			intotoSteps = append(intotoSteps, intotoStep)
		}
	}
	return intotoSteps, nil
}

// Get the task from a pipeline append together to be passed into intoto layout
func getIntotoInspect(p cli.Params, pr v1beta1.PipelineRun) ([]toto.Inspection, error) {
	cs := getClient(p)
	var intotoInsepcts []toto.Inspection
	for taskrunName, _ := range pr.Status.TaskRuns {
		tr, err := taskrun.Get(cs, taskrunName, metav1.GetOptions{}, p.Namespace())
		if err != nil {
			return nil, fmt.Errorf("failed to get tr %s: %v", taskrunName, err)
		}
		for _, step := range tr.Status.Steps {

			intotoInspect := toto.Inspection{
				Type: "Inspect",
				Run:  []string{""},
				SupplyChainItem: toto.SupplyChainItem{
					Name:              step.Name,
					ExpectedMaterials: getExpectedProducts(tr, true),
					//ExpectedProducts:  getExpectedProducts(task),
				},
			}
			intotoInsepcts = append(intotoInsepcts, intotoInspect)
		}

	}
	return intotoInsepcts, nil
}

// Get the commands and arguments from a pipeline step and append together to be passed into intoto layout
func getExpectedCommand(step v1beta1.Step) []string {
	var combined []string

	// combine the command and arguments into one slice
	combined = append(combined, step.Command...)
	combined = append(combined, step.Args...)

	return combined
}

func gitInfo(tr *v1beta1.TaskRun) (commit string, url string) {
	// Scan for git params to use for materials
	for _, p := range tr.Spec.Params {
		if p.Name == commitParam {
			commit = p.Value.StringVal
			continue
		}
		if p.Name == urlParam {
			url = p.Value.StringVal
			// make sure url is PURL (git+https)
			if !strings.HasPrefix(url, "git+") {
				url = "git+" + url
			}
		}
	}
	return
}

// Get the materials from a pipeline task and append together to be passed into intoto layout
func getExpectedMaterials(trs v1beta1.StepState) [][]string {
	var expMats [][]string

	// Get each input to be placed into intoto layout file

	uri := getPackageURLDocker(trs.ImageID)
	if uri != "" {
		expMats = append(expMats, [][]string{{"ALLOW", uri}}...)
	}

	/* uri := getPackageURLDocker(trs.ImageID)
	if uri == "" {
		// If input comes from a different step in the task, append this information
		// if len(input.From) > 0 {
		// 	expMats = append(expMats, [][]string{{"MATCH", uri,
		// 		"WITH", "PRODUCTS", "FROM", strings.Join(input.From, " ")}}...)
		// } else {
		expMats = append(expMats, [][]string{{"ALLOW", uri}}...)
		// }
	} */

	// Append disallow statement to the end of the expected materials
	expMats = append(expMats, [][]string{{"DISALLOW", "*"}}...)

	return expMats
}

// getPackageURLDocker takes an image id and creates a package URL string
// based from it.
// https://github.com/package-url/purl-spec
func getPackageURLDocker(imageID string) string {
	// Default registry per purl spec
	const defaultRegistry = "hub.docker.com"

	// imageID formats: name@alg:digest
	//                  schema://name@alg:digest
	d := strings.Split(imageID, "//")
	if len(d) == 2 {
		// Get away with schema
		imageID = d[1]
	}

	digest, err := name.NewDigest(imageID, name.WithDefaultRegistry(defaultRegistry))
	if err != nil {
		return ""
	}

	// DigestStr() is alg:digest
	parts := strings.Split(digest.DigestStr(), ":")
	if len(parts) != 2 {
		return ""
	}

	purl := fmt.Sprintf("pkg:docker/%s@%s",
		digest.Context().RepositoryStr(),
		digest.DigestStr(),
	)

	// Only inlude registry if it's not the default
	registry := digest.Context().Registry.Name()
	if registry != defaultRegistry {
		purl = fmt.Sprintf("%s?repository_url=%s", purl, registry)
	}

	return purl
}

// getOCIImageID generates an imageID that is compatible imageID field in
// the task result's status field.
func getOCIImageID(name, alg, digest string) string {
	// image id is: docker://name@alg:digest
	return fmt.Sprintf("docker://%s@%s:%s", name, alg, digest)
}

// Get the prodcuts from a pipeline task and append together to be passed into intoto layout
func getExpectedProducts(tr *v1beta1.TaskRun, inspection bool) [][]string {
	var expProds [][]string

	// If not part of the inspection step within the intoto layout grab the matierlas from the input
	if !inspection {

		for _, trr := range tr.Status.TaskRunResults {
			if !strings.HasSuffix(trr.Name, chainsDigestSuffix) {
				continue
			}
			potentialKey := strings.TrimSuffix(trr.Name, chainsDigestSuffix)
			var sub string
			// try to match the key to a param or a result
			for _, p := range tr.Spec.Params {
				if potentialKey != p.Name || p.Value.Type != v1beta1.ParamTypeString {
					continue
				}
				sub = p.Value.StringVal
			}
			for _, p := range tr.Status.TaskRunResults {
				if potentialKey == p.Name {
					sub = strings.TrimRight(p.Value, "\n")
				}
			}
			// if we couldn't match to a param or a result, continue
			if sub == "" {
				continue
			}
			// Value should be of the format:
			//   alg:hash
			//   alg:hash filename
			algHash := strings.Split(trr.Value, " ")[0]
			ah := strings.Split(algHash, ":")
			if len(ah) != 2 {
				continue
			}
			alg := ah[0]
			hash := ah[1]

			// OCI image shall use pacakge url format for subjects
			if trr.Name == ociDigestResult {
				imageID := getOCIImageID(sub, alg, hash)
				sub = getPackageURLDocker(imageID)
			}
			expProds = append(expProds, [][]string{{"ALLOW", sub}}...)

		}
		// Append disallow statement to the end of the expected prodcuts
		//expProds = append(expProds, [][]string{{"DISALLOW", "*"}}...)
		return expProds

	} else {
		// Get each output to be placed into intoto layout file for inspection

		if tr.Spec.Resources == nil {
			return expProds
		}

		// go through resourcesResult
		if tr.Spec.Resources != nil {
			for _, output := range tr.Spec.Resources.Outputs {
				name := output.Name
				if output.PipelineResourceBinding.ResourceSpec == nil {
					continue
				}
				// similarly, we could do this for other pipeline resources or whatever thing replaces them
				if output.PipelineResourceBinding.ResourceSpec.Type == v1alpha1.PipelineResourceTypeImage {
					// get the url and digest, and save as a subject
					// var url, digest string
					for _, s := range tr.Status.ResourcesResult {
						if s.ResourceName == name {
							if s.Key == "url" {
								expProds = append(expProds, [][]string{{"MATCH", s.Value,
									"WITH", "PRODUCTS", "FROM", tr.Name}}...)

							}
							// if s.Key == "digest" {
							// 	digest = s.Value
							// }
						}
					}
				}
			}
		}
		return expProds
	}

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
