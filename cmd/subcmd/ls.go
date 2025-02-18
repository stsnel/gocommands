package subcmd

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	irodsclient_fs "github.com/cyverse/go-irodsclient/fs"
	irodsclient_irodsfs "github.com/cyverse/go-irodsclient/irods/fs"
	irodsclient_types "github.com/cyverse/go-irodsclient/irods/types"
	"github.com/cyverse/gocommands/cmd/flag"
	"github.com/cyverse/gocommands/commons"
	"github.com/dustin/go-humanize"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/xerrors"
)

// FlatReplica is a struct containing a replica and its data object. Used so that we can easily sort
// replicas by either replica properties or data object properties.
type FlatReplica struct {
	Replica    *irodsclient_types.IRODSReplica
	DataObject *irodsclient_types.IRODSDataObject
}

var lsCmd = &cobra.Command{
	Use:     "ls [collection1] [collection2] ...",
	Aliases: []string{"ils", "list"},
	Short:   "List entries in iRODS collections",
	Long:    `This lists data objects and collections in iRODS collections.`,
	RunE:    processLsCommand,
	Args:    cobra.ArbitraryArgs,
}

func AddLsCommand(rootCmd *cobra.Command) {
	// attach common flags
	flag.SetCommonFlags(lsCmd, false)

	flag.SetListFlags(lsCmd)
	flag.SetTicketAccessFlags(lsCmd)
	flag.SetDecryptionFlags(lsCmd)
	flag.SetHiddenFileFlags(lsCmd)

	rootCmd.AddCommand(lsCmd)
}

func processLsCommand(command *cobra.Command, args []string) error {
	ls, err := NewLsCommand(command, args)
	if err != nil {
		return err
	}

	return ls.Process()
}

type LsCommand struct {
	command *cobra.Command

	commonFlagValues       *flag.CommonFlagValues
	listFlagValues         *flag.ListFlagValues
	ticketAccessFlagValues *flag.TicketAccessFlagValues
	decryptionFlagValues   *flag.DecryptionFlagValues
	hiddenFileFlagValues   *flag.HiddenFileFlagValues

	account    *irodsclient_types.IRODSAccount
	filesystem *irodsclient_fs.FileSystem

	sourcePaths []string
}

func NewLsCommand(command *cobra.Command, args []string) (*LsCommand, error) {
	ls := &LsCommand{
		command: command,

		commonFlagValues:       flag.GetCommonFlagValues(command),
		listFlagValues:         flag.GetListFlagValues(),
		ticketAccessFlagValues: flag.GetTicketAccessFlagValues(),
		decryptionFlagValues:   flag.GetDecryptionFlagValues(command),
		hiddenFileFlagValues:   flag.GetHiddenFileFlagValues(),
	}

	// path
	ls.sourcePaths = args[:]

	if len(args) == 0 {
		ls.sourcePaths = []string{"."}
	}

	return ls, nil
}

func (ls *LsCommand) Process() error {
	logger := log.WithFields(log.Fields{
		"package":  "subcmd",
		"struct":   "LsCommand",
		"function": "Process",
	})

	cont, err := flag.ProcessCommonFlags(ls.command)
	if err != nil {
		return xerrors.Errorf("failed to process common flags: %w", err)
	}

	if !cont {
		return nil
	}

	// handle local flags
	_, err = commons.InputMissingFields()
	if err != nil {
		return xerrors.Errorf("failed to input missing fields: %w", err)
	}

	// Create a file system
	ls.account = commons.GetSessionConfig().ToIRODSAccount()
	if len(ls.ticketAccessFlagValues.Name) > 0 {
		logger.Debugf("use ticket: %q", ls.ticketAccessFlagValues.Name)
		ls.account.Ticket = ls.ticketAccessFlagValues.Name
	}

	ls.filesystem, err = commons.GetIRODSFSClientForSingleOperation(ls.account)
	if err != nil {
		return xerrors.Errorf("failed to get iRODS FS Client: %w", err)
	}
	defer ls.filesystem.Release()

	// set default key for decryption
	if len(ls.decryptionFlagValues.Key) == 0 {
		ls.decryptionFlagValues.Key = ls.account.Password
	}

	// Expand wildcards
	if ls.listFlagValues.WildcardExpansion {
		expanded_results, err := commons.ExpandWildcards(ls.filesystem, ls.account, ls.sourcePaths, true, true)
		if err != nil {
			return xerrors.Errorf("failed to expand wildcards:  %w", err)
		}
		ls.sourcePaths = expanded_results
	}

	// run
	for _, sourcePath := range ls.sourcePaths {
		err = ls.listOne(sourcePath)
		if err != nil {
			return xerrors.Errorf("failed to list path %q: %w", sourcePath, err)
		}
	}

	return nil
}

func (ls *LsCommand) requireDecryption(sourcePath string) bool {
	if ls.decryptionFlagValues.NoDecryption {
		return false
	}

	if !ls.decryptionFlagValues.Decryption {
		return false
	}

	mode := commons.DetectEncryptionMode(sourcePath)
	return mode != commons.EncryptionModeUnknown
}

func (ls *LsCommand) listOne(sourcePath string) error {
	cwd := commons.GetCWD()
	home := commons.GetHomeDir()
	zone := ls.account.ClientZone
	sourcePath = commons.MakeIRODSPath(cwd, home, zone, sourcePath)

	sourceEntry, err := ls.filesystem.Stat(sourcePath)
	if err != nil {
		if !irodsclient_types.IsFileNotFoundError(err) {
			return xerrors.Errorf("failed to find data-object/collection %q: %w", sourcePath, err)
		}

		return xerrors.Errorf("failed to stat %q: %w", sourcePath, err)
	}

	connection, err := ls.filesystem.GetMetadataConnection()
	if err != nil {
		return xerrors.Errorf("failed to get connection: %w", err)
	}
	defer ls.filesystem.ReturnMetadataConnection(connection)

	if sourceEntry.IsDir() {
		// collection
		collection, err := irodsclient_irodsfs.GetCollection(connection, sourcePath)
		if err != nil {
			return xerrors.Errorf("failed to get collection %q: %w", sourcePath, err)
		}

		colls, err := irodsclient_irodsfs.ListSubCollections(connection, sourcePath)
		if err != nil {
			return xerrors.Errorf("failed to list sub-collections in %q: %w", sourcePath, err)
		}

		objs, err := irodsclient_irodsfs.ListDataObjects(connection, collection)
		if err != nil {
			return xerrors.Errorf("failed to list data-objects in %q: %w", sourcePath, err)
		}

		// filter out hidden files
		filtered_colls := ls.filterHiddenCollections(colls)
		filtered_objs := ls.filterHiddenDataObjects(objs)

		ls.printDataObjects(filtered_objs)
		ls.printCollections(filtered_colls)

		return nil
	}

	// data object
	entry, err := irodsclient_irodsfs.GetDataObjectWithoutCollection(connection, sourcePath)
	if err != nil {
		return xerrors.Errorf("failed to get data-object %q: %w", sourcePath, err)
	}

	entries := []*irodsclient_types.IRODSDataObject{entry}
	ls.printDataObjects(entries)

	return nil
}

func (ls *LsCommand) filterHiddenCollections(entries []*irodsclient_types.IRODSCollection) []*irodsclient_types.IRODSCollection {
	if !ls.hiddenFileFlagValues.Exclude {
		return entries
	}

	filteredEntries := []*irodsclient_types.IRODSCollection{}

	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name, ".") {
			// not hidden
			filteredEntries = append(filteredEntries, entry)
		}
	}

	return filteredEntries
}

func (ls *LsCommand) filterHiddenDataObjects(entries []*irodsclient_types.IRODSDataObject) []*irodsclient_types.IRODSDataObject {
	if !ls.hiddenFileFlagValues.Exclude {
		return entries
	}

	filteredEntries := []*irodsclient_types.IRODSDataObject{}

	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name, ".") {
			// not hidden
			filteredEntries = append(filteredEntries, entry)
		}
	}

	return filteredEntries
}

func (ls *LsCommand) printCollections(entries []*irodsclient_types.IRODSCollection) {
	sort.SliceStable(entries, ls.getCollectionSortFunction(entries, ls.listFlagValues.SortOrder, ls.listFlagValues.SortReverse))
	for _, entry := range entries {
		commons.Printf("  C- %s\n", entry.Path)
	}
}

func (ls *LsCommand) printDataObjects(entries []*irodsclient_types.IRODSDataObject) {
	if ls.listFlagValues.Format == commons.ListFormatNormal {
		sort.SliceStable(entries, ls.getDataObjectSortFunction(entries, ls.listFlagValues.SortOrder, ls.listFlagValues.SortReverse))
		for _, entry := range entries {
			ls.printDataObjectShort(entry)
		}
	} else {
		replicas := ls.flattenReplicas(entries)
		sort.SliceStable(replicas, ls.getFlatReplicaSortFunction(replicas, ls.listFlagValues.SortOrder, ls.listFlagValues.SortReverse))
		ls.printReplicas(replicas)
	}
}

func (ls *LsCommand) flattenReplicas(objects []*irodsclient_types.IRODSDataObject) []*FlatReplica {
	var result []*FlatReplica
	for _, object := range objects {
		for _, replica := range object.Replicas {
			flatReplica := FlatReplica{
				DataObject: object,
				Replica:    replica,
			}
			result = append(result, &flatReplica)
		}
	}
	return result
}

func (ls *LsCommand) getFlatReplicaSortFunction(entries []*FlatReplica, sortOrder commons.ListSortOrder, sortReverse bool) func(i int, j int) bool {
	if sortReverse {
		switch sortOrder {
		case commons.ListSortOrderName:
			return func(i int, j int) bool {
				return entries[i].DataObject.Name > entries[j].DataObject.Name
			}
		case commons.ListSortOrderExt:
			return func(i int, j int) bool {
				return (path.Ext(entries[i].DataObject.Name) > path.Ext(entries[j].DataObject.Name)) ||
					(path.Ext(entries[i].DataObject.Name) == path.Ext(entries[j].DataObject.Name) &&
						entries[i].DataObject.Name < entries[j].DataObject.Name)
			}
		case commons.ListSortOrderTime:
			return func(i int, j int) bool {
				return (entries[i].Replica.ModifyTime.After(entries[j].Replica.ModifyTime)) ||
					(entries[i].Replica.ModifyTime.Equal(entries[j].Replica.ModifyTime) &&
						entries[i].DataObject.Name < entries[j].DataObject.Name)
			}
		case commons.ListSortOrderSize:
			return func(i int, j int) bool {
				return (entries[i].DataObject.Size > entries[j].DataObject.Size) ||
					(entries[i].DataObject.Size == entries[j].DataObject.Size &&
						entries[i].DataObject.Name < entries[j].DataObject.Name)
			}
		default:
			return func(i int, j int) bool {
				return entries[i].DataObject.Name > entries[j].DataObject.Name
			}
		}
	}

	switch sortOrder {
	case commons.ListSortOrderName:
		return func(i int, j int) bool {
			return entries[i].DataObject.Name < entries[j].DataObject.Name
		}
	case commons.ListSortOrderExt:
		return func(i int, j int) bool {
			return (path.Ext(entries[i].DataObject.Name) < path.Ext(entries[j].DataObject.Name)) ||
				(path.Ext(entries[i].DataObject.Name) == path.Ext(entries[j].DataObject.Name) &&
					entries[i].DataObject.Name < entries[j].DataObject.Name)
		}
	case commons.ListSortOrderTime:
		return func(i int, j int) bool {
			return (entries[i].Replica.ModifyTime.Before(entries[j].Replica.ModifyTime)) ||
				(entries[i].Replica.ModifyTime.Equal(entries[j].Replica.ModifyTime) &&
					entries[i].DataObject.Name < entries[j].DataObject.Name)
		}
	case commons.ListSortOrderSize:
		return func(i int, j int) bool {
			return (entries[i].DataObject.Size < entries[j].DataObject.Size) ||
				(entries[i].DataObject.Size == entries[j].DataObject.Size &&
					entries[i].DataObject.Name < entries[j].DataObject.Name)
		}
	default:
		return func(i int, j int) bool {
			return entries[i].DataObject.Name < entries[j].DataObject.Name
		}
	}
}

func (ls *LsCommand) getDataObjectSortFunction(entries []*irodsclient_types.IRODSDataObject, sortOrder commons.ListSortOrder, sortReverse bool) func(i int, j int) bool {
	if sortReverse {
		switch sortOrder {
		case commons.ListSortOrderName:
			return func(i int, j int) bool {
				return entries[i].Name > entries[j].Name
			}
		case commons.ListSortOrderExt:
			return func(i int, j int) bool {
				return (path.Ext(entries[i].Name) > path.Ext(entries[j].Name)) ||
					(path.Ext(entries[i].Name) == path.Ext(entries[j].Name) &&
						entries[i].Name < entries[j].Name)
			}
		case commons.ListSortOrderTime:
			return func(i int, j int) bool {
				return (ls.getDataObjectModifyTime(entries[i]).After(ls.getDataObjectModifyTime(entries[j]))) ||
					(ls.getDataObjectModifyTime(entries[i]).Equal(ls.getDataObjectModifyTime(entries[j])) &&
						entries[i].Name < entries[j].Name)
			}
		case commons.ListSortOrderSize:
			return func(i int, j int) bool {
				return (entries[i].Size > entries[j].Size) ||
					(entries[i].Size == entries[j].Size &&
						entries[i].Name < entries[j].Name)
			}
		default:
			return func(i int, j int) bool {
				return entries[i].Name > entries[j].Name
			}
		}
	}

	switch sortOrder {
	case commons.ListSortOrderName:
		return func(i int, j int) bool {
			return entries[i].Name < entries[j].Name
		}
	case commons.ListSortOrderExt:
		return func(i int, j int) bool {
			return (path.Ext(entries[i].Name) < path.Ext(entries[j].Name)) ||
				(path.Ext(entries[i].Name) == path.Ext(entries[j].Name) &&
					entries[i].Name < entries[j].Name)
		}
	case commons.ListSortOrderTime:
		return func(i int, j int) bool {
			return (ls.getDataObjectModifyTime(entries[i]).Before(ls.getDataObjectModifyTime(entries[j]))) ||
				(ls.getDataObjectModifyTime(entries[i]).Equal(ls.getDataObjectModifyTime(entries[j])) &&
					entries[i].Name < entries[j].Name)
		}
	case commons.ListSortOrderSize:
		return func(i int, j int) bool {
			return (entries[i].Size < entries[j].Size) ||
				(entries[i].Size == entries[j].Size &&
					entries[i].Name < entries[j].Name)
		}
	default:
		return func(i int, j int) bool {
			return entries[i].Name < entries[j].Name
		}
	}
}

func (ls *LsCommand) getDataObjectModifyTime(object *irodsclient_types.IRODSDataObject) time.Time {
	// ModifyTime of data object is considered to be ModifyTime of replica modified most recently
	maxTime := object.Replicas[0].ModifyTime
	for _, t := range object.Replicas[1:] {
		if t.ModifyTime.After(maxTime) {
			maxTime = t.ModifyTime
		}
	}
	return maxTime
}

func (ls *LsCommand) printDataObjectShort(entry *irodsclient_types.IRODSDataObject) {
	logger := log.WithFields(log.Fields{
		"package":  "subcmd",
		"struct":   "LsCommand",
		"function": "printDataObjectShort",
	})

	newName := entry.Name

	if ls.requireDecryption(entry.Path) {
		// need to decrypt
		encryptionMode := commons.DetectEncryptionMode(newName)
		if encryptionMode != commons.EncryptionModeUnknown {
			encryptManager := ls.getEncryptionManagerForDecryption(encryptionMode)

			decryptedFilename, err := encryptManager.DecryptFilename(newName)
			if err != nil {
				logger.Debugf("%+v", err)
				newName = fmt.Sprintf("%s\t(decryption_failed)", newName)
			} else {
				newName = fmt.Sprintf("%s\t(encrypted: %q)", newName, decryptedFilename)
			}
		}
	}

	commons.Printf("  %s\n", newName)
}

func (ls *LsCommand) printReplicas(flatReplicas []*FlatReplica) {
	for _, flatReplica := range flatReplicas {
		ls.printReplica(*flatReplica)
	}
}

func (ls *LsCommand) getEncryptionManagerForDecryption(mode commons.EncryptionMode) *commons.EncryptionManager {
	manager := commons.NewEncryptionManager(mode)

	switch mode {
	case commons.EncryptionModeWinSCP, commons.EncryptionModePGP:
		manager.SetKey([]byte(ls.decryptionFlagValues.Key))
	case commons.EncryptionModeSSH:
		manager.SetPublicPrivateKey(ls.decryptionFlagValues.PrivateKeyPath)
	}

	return manager
}

func (ls *LsCommand) printReplica(flatReplica FlatReplica) {
	logger := log.WithFields(log.Fields{
		"package":  "subcmd",
		"struct":   "LsCommand",
		"function": "printReplica",
	})

	newName := flatReplica.DataObject.Name

	if ls.requireDecryption(flatReplica.DataObject.Path) {
		// need to decrypt
		encryptionMode := commons.DetectEncryptionMode(newName)
		if encryptionMode != commons.EncryptionModeUnknown {
			encryptManager := ls.getEncryptionManagerForDecryption(encryptionMode)

			decryptedFilename, err := encryptManager.DecryptFilename(newName)
			if err != nil {
				logger.Debugf("%+v", err)
				newName = fmt.Sprintf("%s\tdecryption_failed", newName)
			} else {
				newName = fmt.Sprintf("%s\t(encrypted: %q", newName, decryptedFilename)
			}
		}
	}

	size := fmt.Sprintf("%v", flatReplica.DataObject.Size)
	if ls.listFlagValues.HumanReadableSizes {
		size = humanize.Bytes(uint64(flatReplica.DataObject.Size))
	}

	switch ls.listFlagValues.Format {
	case commons.ListFormatNormal:
		commons.Printf("  %d\t%s\n", flatReplica.Replica.Number, newName)
	case commons.ListFormatLong:
		modTime := commons.MakeDateTimeString(flatReplica.Replica.ModifyTime)
		commons.Printf("  %s\t%d\t%s\t%s\t%s\t%s\t%s\n", flatReplica.Replica.Owner, flatReplica.Replica.Number, flatReplica.Replica.ResourceHierarchy,
			size, modTime, ls.getStatusMark(flatReplica.Replica.Status), newName)
	case commons.ListFormatVeryLong:
		modTime := commons.MakeDateTimeString(flatReplica.Replica.ModifyTime)
		commons.Printf("  %s\t%d\t%s\t%s\t%s\t%s\t%s\n", flatReplica.Replica.Owner, flatReplica.Replica.Number, flatReplica.Replica.ResourceHierarchy,
			size, modTime, ls.getStatusMark(flatReplica.Replica.Status), newName)
		commons.Printf("    %s\t%s\n", flatReplica.Replica.Checksum.IRODSChecksumString, flatReplica.Replica.Path)
	default:
		commons.Printf("  %d\t%s\n", flatReplica.Replica.Number, newName)
	}
}

func (ls *LsCommand) getCollectionSortFunction(entries []*irodsclient_types.IRODSCollection, sortOrder commons.ListSortOrder, sortReverse bool) func(i int, j int) bool {
	if sortReverse {
		switch sortOrder {
		case commons.ListSortOrderName:
			return func(i int, j int) bool {
				return entries[i].Name > entries[j].Name
			}
		case commons.ListSortOrderTime:
			return func(i int, j int) bool {
				return (entries[i].ModifyTime.After(entries[j].ModifyTime)) ||
					(entries[i].ModifyTime.Equal(entries[j].ModifyTime) &&
						entries[i].Name < entries[j].Name)
			}
		// Cannot sort collections by size or extension, so use default sort by name
		default:
			return func(i int, j int) bool {
				return entries[i].Name < entries[j].Name
			}
		}
	}

	switch sortOrder {
	case commons.ListSortOrderName:
		return func(i int, j int) bool {
			return entries[i].Name < entries[j].Name
		}
	case commons.ListSortOrderTime:
		return func(i int, j int) bool {
			return (entries[i].ModifyTime.Before(entries[j].ModifyTime)) ||
				(entries[i].ModifyTime.Equal(entries[j].ModifyTime) &&
					entries[i].Name < entries[j].Name)

		}
		// Cannot sort collections by size or extension, so use default sort by name
	default:
		return func(i int, j int) bool {
			return entries[i].Name < entries[j].Name
		}
	}
}

func (ls *LsCommand) getStatusMark(status string) string {
	switch status {
	case "0":
		return "X" // stale
	case "1":
		return "&" // good
	default:
		return "?"
	}
}
