package chains

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tektoncd/cli/pkg/cli"
)

func layoutCommand(p cli.Params) *cobra.Command {
	longHelp := ``

	c := &cobra.Command{
		Use:   "layout",
		Short: "Print layout for a specific pipelinerun",
		Long:  longHelp,
		Annotations: map[string]string{
			"commandType": "main",
		},
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Get the pipeline.
			pipelinerunName := args[0]
			path := args[1]

			// Extract and decode the value of the payload annotation.
			layoutPath, err := createIntotoLayout(p, pipelinerunName, path)
			if err != nil {
				return err
			}

			// Display the result.
			fmt.Println(layoutPath)

			return nil
		},
	}

	return c
}
