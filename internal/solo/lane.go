package solo

// LaneTools is the Solo tool allow list for a Claude Lane.
func LaneTools() []string {
	return []string{
		"mcp__solo__list_processes",
		"mcp__solo__get_process_status",
		"mcp__solo__get_process_output",
		"mcp__solo__search_output",
		"mcp__solo__get_process_ports",
		"mcp__solo__todo_comment_create",
	}
}
