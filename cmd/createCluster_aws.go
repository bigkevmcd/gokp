package cmd

import (
	"os"

	"github.com/christianh814/gokp/cmd/argo"
	"github.com/christianh814/gokp/cmd/capi"
	"github.com/christianh814/gokp/cmd/export"

	"github.com/christianh814/gokp/cmd/github"
	"github.com/christianh814/gokp/cmd/kind"
	"github.com/christianh814/gokp/cmd/templates"
	"github.com/christianh814/gokp/cmd/utils"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// awscreateCmd represents the aws create command
var awscreateCmd = &cobra.Command{
	Use:   "aws",
	Short: "Creates a GOKP Cluster on AWS",
	Long: `Create a GOKP Cluster on AWS. This will build a cluster on AWS using the given
credentials. For example:

gokp create-cluster --cluster-name=mycluster \
--github-token=githubtoken \
--aws-ssh-key=sshkeynameonaws \
--aws-access-key=awsaccesskeyid \
--aws-secret-key=awssecretaccesskey \
--private-repo=true

The aws ssh key must already exist on your account (the installer
doesn't create one for you).`,
	Run: func(cmd *cobra.Command, args []string) {
		// create home dir
		err := os.MkdirAll(os.Getenv("HOME")+"/.gokp", 0775)
		if err != nil {
			log.Fatal(err)
		}
		// Create workdir and set variables based on that
		workDir, err := utils.CreateWorkDir()
		cobra.CheckErr(err)

		KindCfg = workDir + "/" + "kind.kubeconfig"
		// cleanup workdir at the end
		defer os.RemoveAll(workDir)

		// Grab repo related flags
		ghToken, _ := cmd.Flags().GetString("github-token")
		clusterName, _ := cmd.Flags().GetString("cluster-name")
		privateRepo, _ := cmd.Flags().GetBool("private-repo")

		// Grab AWS related flags
		awsRegion, _ := cmd.Flags().GetString("aws-region")
		awsAccessKey, _ := cmd.Flags().GetString("aws-access-key")
		awsSecretKey, _ := cmd.Flags().GetString("aws-secret-key")
		awsSSHKey, _ := cmd.Flags().GetString("aws-ssh-key")
		awsCPMachine, _ := cmd.Flags().GetString("aws-control-plane-machine")
		awsWMachine, _ := cmd.Flags().GetString("aws-node-machine")
		skipCloudFormation, _ := cmd.Flags().GetBool("skip-cloud-formation")

		CapiCfg := workDir + "/" + clusterName + ".kubeconfig"
		gokpartifacts := os.Getenv("HOME") + "/.gokp/" + clusterName

		tcpName := "gokp-bootstrapper"

		// Run PreReq Checks
		_, err = utils.CheckPreReqs(gokpartifacts)
		if err != nil {
			log.Fatal(err)
		}

		// Create KIND instance
		log.Info("Creating temporary control plane")
		err = kind.CreateKindCluster(tcpName, KindCfg)
		if err != nil {
			log.Fatal(err)
		}

		// Create CAPI instance on AWS
		awsCredsMap := map[string]string{
			"AWS_REGION":                     awsRegion,
			"AWS_ACCESS_KEY_ID":              awsAccessKey,
			"AWS_SECRET_ACCESS_KEY":          awsSecretKey,
			"AWS_SSH_KEY_NAME":               awsSSHKey,
			"AWS_CONTROL_PLANE_MACHINE_TYPE": awsCPMachine,
			"AWS_NODE_MACHINE_TYPE":          awsWMachine,
		}

		// By default, create an HA Cluster
		haCluster := true
		_, err = capi.CreateAwsK8sInstance(KindCfg, &clusterName, workDir, awsCredsMap, CapiCfg, haCluster, skipCloudFormation)
		if err != nil {
			log.Fatal(err)
		}

		// Create the GitOps repo
		_, gitopsrepo, err := github.CreateRepo(&clusterName, ghToken, &privateRepo, workDir)
		if err != nil {
			log.Fatal(err)
		}

		// Create repo dir structure. Including Argo CD install YAMLs and base YAMLs. Push initial dir structure out
		_, err = templates.CreateRepoSkel(&clusterName, workDir, ghToken, gitopsrepo, &privateRepo)
		if err != nil {
			log.Fatal(err)
		}

		// Export/Create Cluster YAML to the Repo, Make sure kustomize is used for the core components
		log.Info("Exporting Cluster YAML")
		_, err = export.ExportClusterYaml(CapiCfg, workDir+"/"+clusterName)
		if err != nil {
			log.Fatal(err)
		}

		// Git push newly exported YAML to GitOps repo
		privateKeyFile := workDir + "/" + clusterName + "_rsa"
		_, err = github.CommitAndPush(workDir+"/"+clusterName, privateKeyFile, "exporting existing YAML")
		if err != nil {
			log.Fatal(err)
		}

		// Install Argo CD on the newly created cluster
		// Deploy applications/applicationsets
		log.Info("Deploying Argo CD GitOps Controller")
		_, err = argo.BootstrapArgoCD(&clusterName, workDir, CapiCfg)
		if err != nil {
			log.Fatal(err)
		}

		// MOVE from kind to capi instance
		//	uses the kubeconfig files of "src ~> dest"
		log.Info("Moving CAPI Artifacts to: " + clusterName)
		_, err = capi.MoveMgmtCluster(KindCfg, CapiCfg)
		if err != nil {
			log.Fatal(err)
		}

		// Delete local Kind Cluster
		log.Info("Deleting temporary control plane")
		err = kind.DeleteKindCluster(tcpName, KindCfg)
		if err != nil {
			log.Fatal(err)
		}

		// Move components to ~/.gokp/<clustername> and remove stuff you don't need to know.
		// 	TODO: this is ugly and will refactor this later
		///err = utils.CopyDir(workDir, gokpartifacts)
		err = os.Rename(workDir, gokpartifacts)
		if err != nil {
			log.Fatal(err)
		}

		notNeededDirs := []string{
			"argocd-install-output",
			"capi-install-yamls-output",
			"cni-output",
		}

		for _, notNeededDir := range notNeededDirs {
			err = os.RemoveAll(gokpartifacts + "/" + notNeededDir)
			if err != nil {
				log.Fatal(err)
			}
		}

		notNeededFiles := []string{
			"argocd-install.yaml",
			"cni.yaml",
			"install-cluster.yaml",
			"kind.kubeconfig",
		}

		for _, notNeededFile := range notNeededFiles {
			err = os.Remove(gokpartifacts + "/" + notNeededFile)
			if err != nil {
				log.Fatal(err)
			}
		}

		// Give info
		log.Info("Cluster Successfully installed! Everything you need is under: ~/.gokp/", clusterName)

	},
}

func init() {
	createClusterCmd.AddCommand(awscreateCmd)

	// Repo specific flags
	awscreateCmd.Flags().String("github-token", "", "GitHub token to use.")
	awscreateCmd.Flags().String("cluster-name", "", "Name of your cluster.")
	awscreateCmd.Flags().BoolP("private-repo", "", true, "Create a private repo.")

	//AWS Specific flags
	awscreateCmd.Flags().String("aws-region", "us-east-1", "Which region to deploy to.")
	awscreateCmd.Flags().String("aws-access-key", "", "Your AWS Access Key.")
	awscreateCmd.Flags().String("aws-secret-key", "", "Your AWS Secret Key.")
	awscreateCmd.Flags().String("aws-ssh-key", "default", "The SSH key in AWS that you want to use for the instances.")
	awscreateCmd.Flags().String("aws-control-plane-machine", "m4.xlarge", "The AWS instance type for the Control Plane")
	awscreateCmd.Flags().String("aws-node-machine", "m4.xlarge", "The AWS instance type for the Worker instances")
	awscreateCmd.Flags().BoolP("skip-cloud-formation", "", false, "Skip the creation of the CloudFormation Template.")

	// require the following flags
	awscreateCmd.MarkFlagRequired("github-token")
	awscreateCmd.MarkFlagRequired("cluster-name")
	awscreateCmd.MarkFlagRequired("aws-access-key")
	awscreateCmd.MarkFlagRequired("aws-secret-key")
}
