// tfsync is the tfsync CLI: list workspaces, trigger sync, show plan output.
package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tfsyncv1alpha1 "github.com/tfsync/tfsync/api/v1alpha1"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(tfsyncv1alpha1.AddToScheme(scheme))
}

func main() {
	root := &cobra.Command{Use: "tfsync", Short: "tfsync CLI"}
	var ns string
	root.PersistentFlags().StringVarP(&ns, "namespace", "n", "default", "namespace to target")

	root.AddCommand(cmdList(&ns), cmdSync(&ns), cmdPlan(&ns))
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func newClient() (client.Client, error) {
	cfg, err := ctrl.GetConfig()
	if err != nil {
		return nil, err
	}
	return client.New(cfg, client.Options{Scheme: scheme})
}

func cmdList(ns *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List workspaces in the target namespace",
		RunE: func(c *cobra.Command, _ []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			var list tfsyncv1alpha1.WorkspaceList
			if err := cl.List(context.TODO(), &list, client.InNamespace(*ns)); err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			defer w.Flush()
			fmt.Fprintln(w, "NAME\tPHASE\tREPO\tBRANCH\tLAST APPLIED")
			for _, ws := range list.Items {
				last := "-"
				if ws.Status.LastAppliedAt != nil {
					last = duration(time.Since(ws.Status.LastAppliedAt.Time)) + " ago"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					ws.Name, ws.Status.Phase, ws.Spec.Source.Repo, ws.Spec.Source.Branch, last)
			}
			return nil
		},
	}
}

func cmdSync(ns *string) *cobra.Command {
	return &cobra.Command{
		Use:   "sync <workspace>",
		Short: "Trigger an immediate reconcile by clearing lastAppliedAt",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			var ws tfsyncv1alpha1.Workspace
			if err := cl.Get(context.TODO(), client.ObjectKey{Namespace: *ns, Name: args[0]}, &ws); err != nil {
				return err
			}
			ws.Status.LastAppliedAt = nil
			ws.Status.Phase = tfsyncv1alpha1.PhasePending
			// Annotate to nudge the cache and surface intent in events/audit.
			if ws.Annotations == nil {
				ws.Annotations = map[string]string{}
			}
			ws.Annotations["tfsync.io/sync-requested-at"] = metav1.Now().Format(time.RFC3339)
			if err := cl.Update(context.TODO(), &ws); err != nil {
				return err
			}
			if err := cl.Status().Update(context.TODO(), &ws); err != nil {
				return err
			}
			fmt.Printf("sync requested for %s/%s\n", *ns, args[0])
			return nil
		},
	}
}

func cmdPlan(ns *string) *cobra.Command {
	return &cobra.Command{
		Use:   "plan <workspace>",
		Short: "Print the last plan output for a workspace",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			cl, err := newClient()
			if err != nil {
				return err
			}
			var ws tfsyncv1alpha1.Workspace
			if err := cl.Get(context.TODO(), client.ObjectKey{Namespace: *ns, Name: args[0]}, &ws); err != nil {
				return err
			}
			if ws.Status.LastPlanOutput == "" {
				fmt.Println("(no plan output yet)")
				return nil
			}
			fmt.Println(ws.Status.LastPlanOutput)
			return nil
		},
	}
}

func duration(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
}
