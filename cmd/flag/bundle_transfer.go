package flag

import (
	"os"
	"strconv"

	"github.com/cyverse/gocommands/commons"
	"github.com/spf13/cobra"
)

type BundleTransferFlagValues struct {
	LocalTempPath      string
	IRODSTempPath      string
	ClearOld           bool
	MinFileNum         int
	MaxFileNum         int
	MaxFileSize        int64
	NoBulkRegistration bool
	maxFileSizeInput   string
}

var (
	bundleTransferFlagValues BundleTransferFlagValues
)

func SetBundleTransferFlags(command *cobra.Command, hideTempPathConfig bool, hideTransferConfig bool) {
	command.Flags().StringVar(&bundleTransferFlagValues.LocalTempPath, "local_temp", os.TempDir(), "Specify local temp directory path to create bundle files")
	command.Flags().StringVar(&bundleTransferFlagValues.IRODSTempPath, "irods_temp", "", "Specify iRODS temp collection path to upload bundle files to")
	command.Flags().BoolVar(&bundleTransferFlagValues.ClearOld, "clear", false, "Clear stale bundle files")
	command.Flags().IntVar(&bundleTransferFlagValues.MinFileNum, "min_file_num", commons.MinBundleFileNumDefault, "Specify min file number in a bundle file")
	command.Flags().IntVar(&bundleTransferFlagValues.MaxFileNum, "max_file_num", commons.MaxBundleFileNumDefault, "Specify max file number in a bundle file")
	command.Flags().StringVar(&bundleTransferFlagValues.maxFileSizeInput, "max_file_size", strconv.FormatInt(commons.MaxBundleFileSizeDefault, 10), "Specify max file size of a bundle file")
	command.Flags().BoolVar(&bundleTransferFlagValues.NoBulkRegistration, "no_bulk_reg", false, "Disable bulk registration")

	if hideTempPathConfig {
		command.Flags().MarkHidden("local_temp")
		command.Flags().MarkHidden("irods_temp")
	}

	if hideTransferConfig {
		command.Flags().MarkHidden("clear")
		command.Flags().MarkHidden("min_file_num")
		command.Flags().MarkHidden("max_file_num")
		command.Flags().MarkHidden("max_file_size")
		command.Flags().MarkHidden("no_bulk_reg")
	}
}

func GetBundleTransferFlagValues() *BundleTransferFlagValues {
	size, _ := commons.ParseSize(bundleTransferFlagValues.maxFileSizeInput)
	bundleTransferFlagValues.MaxFileSize = size

	return &bundleTransferFlagValues
}
