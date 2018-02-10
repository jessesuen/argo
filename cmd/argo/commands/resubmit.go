package commands

import (
	"log"
	"os"

	"github.com/argoproj/argo/workflow/common"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	RootCmd.AddCommand(resubmitCmd)
}

var resubmitCmd = &cobra.Command{
	Use:   "resubmit WORKFLOW",
	Short: "resubmit a workflow and reuse outputs from previous run (EXPERIMENTAL)",
	Run:   ResubmitWorkflow,
}

// ResubmitWorkflow resubmits a previous workflow
func ResubmitWorkflow(cmd *cobra.Command, args []string) {
	if len(args) == 0 {
		cmd.HelpFunc()(cmd, args)
		os.Exit(1)
	}

	wfClient := InitWorkflowClient()
	wf, err := wfClient.Get(args[0], metav1.GetOptions{})
	if err != nil {
		log.Fatal(err)
	}
	newWF, err := common.GenerateResubmitWorkflow(wf)
	if err != nil {
		log.Fatal(err)
	}
	submitWorkflow(newWF)
}
