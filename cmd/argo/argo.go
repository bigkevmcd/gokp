package argo

import (
	"context"
	"os"
	"path/filepath"

	"github.com/christianh814/project-spichern/cmd/capi"
	"github.com/christianh814/project-spichern/cmd/utils"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/kyaml/filesys"
)

// BootstrapArgoCD installs ArgoCD on a given cluster with the provided Kustomize-ed dir
func BootstrapArgoCD(clustername *string, workdir string, capicfg string) (bool, error) {
	// Set the repoDir path where things should be cloned.
	// check if it exists
	repoDir := workdir + "/" + *clustername
	overlay := repoDir + "/cluster/bootstrap/overlays/default"
	if _, err := os.Stat(repoDir); os.IsNotExist(err) {
		return false, err
	}

	// generate the ArgoCD Install YAML
	argocdyaml := workdir + "/" + "argocd-install.yaml"
	_, err := RunKustomize(overlay, argocdyaml)
	if err != nil {
		return false, err
	}

	// Let's take that YAML and apply it to the created cluster
	// First, let's split this up into smaller files
	err = utils.SplitYamls(workdir+"/"+"argocd-install-output", argocdyaml, "---")
	if err != nil {
		return false, err
	}

	//get a list of those files
	argoInstallYamls, err := filepath.Glob(workdir + "/" + "argocd-install-output" + "/" + "*.yaml")
	if err != nil {
		return false, err
	}

	// Set up a connection to the K8S cluster and apply these bad boys
	capiInstallConfig, err := clientcmd.BuildConfigFromFlags("", capicfg)
	if err != nil {
		return false, err
	}

	// First, create the namespace
	//CHX

	for _, argoInstallYaml := range argoInstallYamls {
		err = capi.DoSSA(context.TODO(), capiInstallConfig, argoInstallYaml)
		if err != nil {
			//log.Warn("Unable to read YAML: ", err)
			return false, err
		}
	}

	return true, nil
}

// RunKustomize runs kustomize on a specific dir and outputs it to a YAML to use for later
func RunKustomize(dir string, outfile string) (bool, error) {
	// set up where to run kustomize, how to write it, and which file to create
	kustomizeDir := dir
	fSys := filesys.MakeFsOnDisk()
	writer, _ := os.Create(outfile)

	// The default options are fine for our use case
	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())

	// Run Kustomize
	m, err := k.Run(fSys, kustomizeDir)
	if err != nil {
		return false, err
	}

	// Convert to YAML
	yml, err := m.AsYaml()
	if err != nil {
		return false, err
	}

	// Write YAML out
	_, err = writer.Write(yml)
	if err != nil {
		return false, err
	}

	// If we're here, we should be okay
	return false, nil
}
