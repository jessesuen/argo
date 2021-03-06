package commands

import (
	"context"
	"os"
	"sort"
	"strings"

	"github.com/argoproj/pkg/errors"
	argotime "github.com/argoproj/pkg/time"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	"github.com/argoproj/argo/cmd/argo/commands/client"
	workflowpkg "github.com/argoproj/argo/pkg/apiclient/workflow"
	wfv1 "github.com/argoproj/argo/pkg/apis/workflow/v1alpha1"
	"github.com/argoproj/argo/util/printer"
	"github.com/argoproj/argo/workflow/common"
)

type listFlags struct {
	namespace     string
	status        []string
	completed     bool
	running       bool
	prefix        string
	output        string
	createdSince  string
	finishedAfter string
	chunkSize     int64
	noHeaders     bool
	labels        string
	fields        string
}

func NewListCommand() *cobra.Command {
	var (
		listArgs      listFlags
		allNamespaces bool
	)
	var command = &cobra.Command{
		Use:   "list",
		Short: "list workflows",
		Run: func(cmd *cobra.Command, args []string) {
			ctx, apiClient := client.NewAPIClient()
			serviceClient := apiClient.NewWorkflowServiceClient()
			if !allNamespaces {
				listArgs.namespace = client.Namespace()
			}
			workflows, err := listWorkflows(ctx, serviceClient, listArgs)
			errors.CheckError(err)
			err = printer.PrintWorkflows(workflows, os.Stdout, printer.PrintOpts{
				NoHeaders: listArgs.noHeaders,
				Namespace: allNamespaces,
				Output:    listArgs.output,
			})
			errors.CheckError(err)
		},
	}
	command.Flags().BoolVar(&allNamespaces, "all-namespaces", false, "Show workflows from all namespaces")
	command.Flags().StringVar(&listArgs.prefix, "prefix", "", "Filter workflows by prefix")
	command.Flags().StringVar(&listArgs.finishedAfter, "older", "", "List completed workflows finished before the specified duration (e.g. 10m, 3h, 1d)")
	command.Flags().StringSliceVar(&listArgs.status, "status", []string{}, "Filter by status (comma separated)")
	command.Flags().BoolVar(&listArgs.completed, "completed", false, "Show only completed workflows")
	command.Flags().BoolVar(&listArgs.running, "running", false, "Show only running workflows")
	command.Flags().StringVarP(&listArgs.output, "output", "o", "", "Output format. One of: wide|name")
	command.Flags().StringVar(&listArgs.createdSince, "since", "", "Show only workflows created after than a relative duration")
	command.Flags().Int64VarP(&listArgs.chunkSize, "chunk-size", "", 0, "Return large lists in chunks rather than all at once. Pass 0 to disable.")
	command.Flags().BoolVar(&listArgs.noHeaders, "no-headers", false, "Don't print headers (default print headers).")
	command.Flags().StringVarP(&listArgs.labels, "selector", "l", "", "Selector (label query) to filter on, supports '=', '==', and '!='.(e.g. -l key1=value1,key2=value2)")
	command.Flags().StringVar(&listArgs.fields, "field-selector", "", "Selector (field query) to filter on, supports '=', '==', and '!='.(e.g. --field-selectorkey1=value1,key2=value2). The server only supports a limited number of field queries per type.")
	return command
}

func listWorkflows(ctx context.Context, serviceClient workflowpkg.WorkflowServiceClient, flags listFlags) (wfv1.Workflows, error) {
	listOpts := &metav1.ListOptions{
		Limit: flags.chunkSize,
	}
	labelSelector := labels.NewSelector()
	if len(flags.status) != 0 {
		req, _ := labels.NewRequirement(common.LabelKeyPhase, selection.In, flags.status)
		if req != nil {
			labelSelector = labelSelector.Add(*req)
		}
	}
	if flags.completed {
		req, _ := labels.NewRequirement(common.LabelKeyCompleted, selection.Equals, []string{"true"})
		labelSelector = labelSelector.Add(*req)
	}
	if flags.running {
		req, _ := labels.NewRequirement(common.LabelKeyCompleted, selection.NotEquals, []string{"true"})
		labelSelector = labelSelector.Add(*req)
	}
	if listOpts.LabelSelector = labelSelector.String(); listOpts.LabelSelector != "" {
		listOpts.LabelSelector = listOpts.LabelSelector + ","
	}
	listOpts.LabelSelector = listOpts.LabelSelector + flags.labels
	listOpts.FieldSelector = flags.fields
	var workflows wfv1.Workflows
	for {
		log.WithField("listOpts", listOpts).Debug()
		wfList, err := serviceClient.ListWorkflows(ctx, &workflowpkg.WorkflowListRequest{Namespace: flags.namespace, ListOptions: listOpts})
		if err != nil {
			return nil, err
		}
		workflows = append(workflows, wfList.Items...)
		if wfList.Continue == "" {
			break
		}
		listOpts.Continue = wfList.Continue
	}
	workflows = workflows.
		Filter(func(wf wfv1.Workflow) bool {
			return strings.HasPrefix(wf.ObjectMeta.Name, flags.prefix)
		})
	if flags.createdSince != "" {
		t, err := argotime.ParseSince(flags.createdSince)
		errors.CheckError(err)
		workflows = workflows.Filter(wfv1.WorkflowCreatedAfter(*t))
	}
	if flags.finishedAfter != "" {
		t, err := argotime.ParseSince(flags.finishedAfter)
		errors.CheckError(err)
		workflows = workflows.Filter(wfv1.WorkflowFinishedBefore(*t))
	}
	sort.Sort(workflows)
	return workflows, nil
}
