package subcmd

import (
	"fmt"
	"sort"

	irodsclient_fs "github.com/cyverse/go-irodsclient/fs"
	"github.com/cyverse/go-irodsclient/irods/types"
	irodsclient_types "github.com/cyverse/go-irodsclient/irods/types"
	"github.com/cyverse/gocommands/cmd/flag"
	"github.com/cyverse/gocommands/commons"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/xerrors"
)

var lsticketCmd = &cobra.Command{
	Use:     "lsticket [ticket_string1] [ticket_string2] ...",
	Aliases: []string{"ls_ticket", "list_ticket"},
	Short:   "List tickets for the user",
	Long:    `This lists tickets for the user.`,
	RunE:    processLsticketCommand,
	Args:    cobra.ArbitraryArgs,
}

func AddLsticketCommand(rootCmd *cobra.Command) {
	// attach common flags
	flag.SetCommonFlags(lsticketCmd, true)

	flag.SetListFlags(lsticketCmd)

	rootCmd.AddCommand(lsticketCmd)
}

func processLsticketCommand(command *cobra.Command, args []string) error {
	lsTicket, err := NewLsTicketCommand(command, args)
	if err != nil {
		return err
	}

	return lsTicket.Process()
}

type LsTicketCommand struct {
	command *cobra.Command

	listFlagValues *flag.ListFlagValues

	account    *irodsclient_types.IRODSAccount
	filesystem *irodsclient_fs.FileSystem

	tickets []string
}

func NewLsTicketCommand(command *cobra.Command, args []string) (*LsTicketCommand, error) {
	lsTicket := &LsTicketCommand{
		command: command,

		listFlagValues: flag.GetListFlagValues(),
	}

	// tickets
	lsTicket.tickets = args

	return lsTicket, nil
}

func (lsTicket *LsTicketCommand) Process() error {
	cont, err := flag.ProcessCommonFlags(lsTicket.command)
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
	lsTicket.account = commons.GetAccount()
	lsTicket.filesystem, err = commons.GetIRODSFSClient(lsTicket.account)
	if err != nil {
		return xerrors.Errorf("failed to get iRODS FS Client: %w", err)
	}
	defer lsTicket.filesystem.Release()

	if len(lsTicket.tickets) == 0 {
		return lsTicket.listTickets()
	}

	for _, ticketName := range lsTicket.tickets {
		err = lsTicket.printTicket(ticketName)
		if err != nil {
			return xerrors.Errorf("failed to print ticket %q: %w", ticketName, err)
		}
	}

	return nil
}

func (lsTicket *LsTicketCommand) listTickets() error {
	tickets, err := lsTicket.filesystem.ListTickets()
	if err != nil {
		return xerrors.Errorf("failed to list tickets: %w", err)
	}

	if len(tickets) == 0 {
		commons.Printf("Found no tickets\n")
	}

	return lsTicket.printTickets(tickets)
}

func (lsTicket *LsTicketCommand) printTicket(ticketName string) error {
	logger := log.WithFields(log.Fields{
		"package":  "subcmd",
		"struct":   "LsTicketCommand",
		"function": "printTicket",
	})

	logger.Debugf("print ticket %q", ticketName)

	ticket, err := lsTicket.filesystem.GetTicket(ticketName)
	if err != nil {
		return xerrors.Errorf("failed to get ticket %q: %w", ticketName, err)
	}

	tickets := []*types.IRODSTicket{ticket}
	return lsTicket.printTickets(tickets)
}

func (lsTicket *LsTicketCommand) printTickets(tickets []*types.IRODSTicket) error {
	sort.SliceStable(tickets, lsTicket.getTicketSortFunction(tickets, lsTicket.listFlagValues.SortOrder, lsTicket.listFlagValues.SortReverse))

	for _, ticket := range tickets {
		err := lsTicket.printTicketInternal(ticket)
		if err != nil {
			return xerrors.Errorf("failed to print ticket %q: %w", ticket, err)
		}
	}

	return nil
}

func (lsTicket *LsTicketCommand) printTicketInternal(ticket *types.IRODSTicket) error {
	fmt.Printf("[%s]\n", ticket.Name)
	fmt.Printf("  id: %d\n", ticket.ID)
	fmt.Printf("  name: %s\n", ticket.Name)
	fmt.Printf("  type: %s\n", ticket.Type)
	fmt.Printf("  owner: %s\n", ticket.Owner)
	fmt.Printf("  owner zone: %s\n", ticket.OwnerZone)
	fmt.Printf("  object type: %s\n", ticket.ObjectType)
	fmt.Printf("  path: %s\n", ticket.Path)
	fmt.Printf("  uses limit: %d\n", ticket.UsesLimit)
	fmt.Printf("  uses count: %d\n", ticket.UsesCount)
	fmt.Printf("  write file limit: %d\n", ticket.WriteFileLimit)
	fmt.Printf("  write file count: %d\n", ticket.WriteFileCount)
	fmt.Printf("  write byte limit: %d\n", ticket.WriteByteLimit)
	fmt.Printf("  write byte count: %d\n", ticket.WriteByteCount)

	if ticket.ExpirationTime.IsZero() {
		fmt.Print("  expiry time: none\n")
	} else {
		fmt.Printf("  expiry time: %s\n", commons.MakeDateTimeString(ticket.ExpirationTime))
	}

	if lsTicket.listFlagValues.Format == commons.ListFormatLong || lsTicket.listFlagValues.Format == commons.ListFormatVeryLong {
		restrictions, err := lsTicket.filesystem.GetTicketRestrictions(ticket.ID)
		if err != nil {
			return xerrors.Errorf("failed to get ticket restrictions %q: %w", ticket.Name, err)
		}

		if restrictions != nil {
			if len(restrictions.AllowedHosts) == 0 {
				fmt.Printf("  No host restrictions\n")
			} else {
				for _, host := range restrictions.AllowedHosts {
					fmt.Printf("  Allowed Hosts:\n")
					fmt.Printf("    - %s\n", host)
				}
			}

			if len(restrictions.AllowedUserNames) == 0 {
				fmt.Printf("  No user restrictions\n")
			} else {
				for _, user := range restrictions.AllowedUserNames {
					fmt.Printf("  Allowed Users:\n")
					fmt.Printf("    - %s\n", user)
				}
			}

			if len(restrictions.AllowedGroupNames) == 0 {
				fmt.Printf("  No group restrictions\n")
			} else {
				for _, group := range restrictions.AllowedGroupNames {
					fmt.Printf("  Allowed Groups:\n")
					fmt.Printf("    - %s\n", group)
				}
			}
		}
	}

	return nil
}

func (lsTicket *LsTicketCommand) getTicketSortFunction(tickets []*types.IRODSTicket, sortOrder commons.ListSortOrder, sortReverse bool) func(i int, j int) bool {
	if sortReverse {
		switch sortOrder {
		case commons.ListSortOrderName:
			return func(i int, j int) bool {
				return tickets[i].Name > tickets[j].Name
			}
		case commons.ListSortOrderTime:
			return func(i int, j int) bool {
				return (tickets[i].ExpirationTime.After(tickets[j].ExpirationTime)) ||
					(tickets[i].ExpirationTime.Equal(tickets[j].ExpirationTime) &&
						tickets[i].Name < tickets[j].Name)
			}
		// Cannot sort tickets by size or extension, so use default sort by name
		default:
			return func(i int, j int) bool {
				return tickets[i].Name < tickets[j].Name
			}
		}
	}

	switch sortOrder {
	case commons.ListSortOrderName:
		return func(i int, j int) bool {
			return tickets[i].Name < tickets[j].Name
		}
	case commons.ListSortOrderTime:
		return func(i int, j int) bool {
			return (tickets[i].ExpirationTime.Before(tickets[j].ExpirationTime)) ||
				(tickets[i].ExpirationTime.Equal(tickets[j].ExpirationTime) &&
					tickets[i].Name < tickets[j].Name)

		}
		// Cannot sort tickets by size or extension, so use default sort by name
	default:
		return func(i int, j int) bool {
			return tickets[i].Name < tickets[j].Name
		}
	}
}
