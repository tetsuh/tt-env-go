package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tetsuh/tt-env-go/pkg/install"
)

var (
	dryRun bool
	force  bool
)

// installCmd represents the install command.
var installCmd = &cobra.Command{
	Use:   "install <release>",
	Short: "Install or upgrade a Tenstorrent stack release",
	Long: `Install a release from its catalog or local manifest, recording the concrete
host package versions in versions/<release>/manifest.json when staged. An
already-installed release is left unchanged unless --force is supplied; --force
replays an existing lock (legacy lockless releases use their manifest).

With --upgrade, re-resolve system and Python packages from configured
repositories and indexes and git components at remote HEAD. Container references
(including digest pins) and the set of optional packages come from the template
manifest and do not change independently. Use --like <release> to choose the
manifest whose structure seeds a new release; without it, the target's own
manifest is used. To refresh an installed release, use --upgrade --force.

The scope summary is also shown by --dry-run, without installing anything.
Capture can name the resulting installed resolution as a local manifest.
Pinned package availability is checked before mutation; DNF uses cached
metadata, so an unconfigured required repository may block installation.`,
	Args: cobra.ExactArgs(1),
	RunE: runInstall,
}

// installFlags reads the new flags and their backwards-compatible aliases.
func installFlags(cmd *cobra.Command) (install.Options, error) {
	upgrade, err := cmd.Flags().GetBool("upgrade")
	if err != nil {
		return install.Options{}, err
	}
	latest, err := cmd.Flags().GetBool("latest")
	if err != nil {
		return install.Options{}, err
	}
	like, err := cmd.Flags().GetString("like")
	if err != nil {
		return install.Options{}, err
	}
	base, err := cmd.Flags().GetString("base")
	if err != nil {
		return install.Options{}, err
	}
	if cmd.Flags().Changed("latest") {
		fmt.Fprintln(cmd.ErrOrStderr(), "Deprecated: --latest is an alias for --upgrade; use --upgrade instead.")
	}
	if cmd.Flags().Changed("base") {
		fmt.Fprintln(cmd.ErrOrStderr(), "Deprecated: --base is an alias for --like; use --like instead.")
	}
	if like != "" && base != "" && like != base {
		return install.Options{}, fmt.Errorf("--like and --base specify different template releases")
	}
	if like == "" {
		like = base
	}
	if like != "" && !upgrade && !latest {
		return install.Options{}, fmt.Errorf("--like requires --upgrade (or its deprecated --latest alias)")
	}
	return install.Options{DryRun: dryRun, Force: force, Upgrade: upgrade || latest, Like: like}, nil
}

// runInstall installs the requested release via the install orchestrator.
func runInstall(cmd *cobra.Command, args []string) error {
	opts, err := installFlags(cmd)
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	out := cmd.OutOrStdout()
	orch := &install.Orchestrator{
		Root: ttHome(),
		Logf: func(format string, a ...any) {
			fmt.Fprintf(out, format+"\n", a...)
		},
	}

	_, err = orch.Install(ctx, args[0], opts)
	return err
}

func init() {
	installCmd.Flags().BoolVar(&dryRun, "dry-run", false, "perform a dry-run without installing")
	installCmd.Flags().BoolVar(&force, "force", false, "force reinstall if release is already installed")
	installCmd.Flags().Bool("upgrade", false, "re-resolve host tooling but keep container references from the manifest")
	installCmd.Flags().String("like", "", "release whose manifest structure seeds an upgrade (defaults to <release>)")
	installCmd.Flags().Bool("latest", false, "deprecated alias for --upgrade")
	installCmd.Flags().String("base", "", "deprecated alias for --like")
	_ = installCmd.Flags().MarkHidden("latest")
	_ = installCmd.Flags().MarkHidden("base")
	RootCmd.AddCommand(installCmd)
}
