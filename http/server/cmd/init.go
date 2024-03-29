package cmd

import (
	"github.com/spf13/cobra"
	"github.com/xx-labs/blockchain-cmix-relay/cmix"
)

// Variables used in init command only
var ndfUrl string
var cert string
var outputFile string

var initCmd = &cobra.Command{
	Use:   "init",
	Args:  cobra.MinimumNArgs(0),
	Short: "Initialize the cMix HTTP Proxy server",
	Long:  `This command initializes a new cMix client, stores the state information, and outputs the contact information to a file`,
	Run: func(cmd *cobra.Command, args []string) {
		// Initialize logging
		initLog()

		// Config
		config := cmix.Config{
			LogPrefix:     logPrefix,
			Cert:          cert,
			NdfUrl:        ndfUrl,
			StatePath:     statePath,
			StatePassword: statePassword,
		}

		// Initialize REST server
		cmix.InitializeServer(config, outputFile)
	},
}

func init() {
	initCmd.Flags().StringVarP(&ndfUrl, "ndf", "d",
		"https://elixxir-bins.s3.us-west-1.amazonaws.com/ndf/mainnet.json",
		"URL used to download NDF file on initialization",
	)
	initCmd.Flags().StringVarP(&cert, "cert", "c",
		"mainnet.crt",
		"Path to certificate file used to verify NDF download",
	)
	initCmd.Flags().StringVarP(&outputFile, "output", "o",
		"http.xxc",
		"Path to output contact file",
	)
	rootCmd.AddCommand(initCmd)
}
