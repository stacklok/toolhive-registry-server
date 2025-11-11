package helpers

import (
	"fmt"
	"time"
)

// RegistryServer represents a server definition in the registry
type RegistryServer struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Tier        string            `json:"tier"`
	Status      string            `json:"status"`
	Transport   string            `json:"transport"`
	Tools       []string          `json:"tools,omitempty"`
	Image       string            `json:"image,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// ToolHiveRegistryData represents the ToolHive registry format
type ToolHiveRegistryData struct {
	Version       string                    `json:"version"`
	LastUpdated   string                    `json:"last_updated"`
	Servers       map[string]RegistryServer `json:"servers"`
	RemoteServers map[string]RegistryServer `json:"remoteServers,omitempty"`
}

// UniqueNames generates unique names for test resources
type UniqueNames struct {
	RegistryName string
	GitRepoName  string
}

// NewUniqueNames creates unique names with a given prefix
func NewUniqueNames(prefix string) *UniqueNames {
	timestamp := time.Now().UnixNano()
	return &UniqueNames{
		RegistryName: fmt.Sprintf("%s-registry-%d", prefix, timestamp),
		GitRepoName:  fmt.Sprintf("%s-repo-%d", prefix, timestamp),
	}
}

// CreateOriginalTestServers creates a basic set of test servers
func CreateOriginalTestServers() []RegistryServer {
	return []RegistryServer{
		{
			Name:        "filesystem",
			Description: "File system operations for secure file access",
			Tier:        "Official",
			Status:      "Active",
			Transport:   "stdio",
			Tools:       []string{"read_file", "write_file", "list_directory"},
			Image:       "ghcr.io/modelcontextprotocol/server-filesystem:latest",
			Tags:        []string{"filesystem", "files"},
		},
	}
}

// CreateUpdatedTestServers creates an updated set of test servers
func CreateUpdatedTestServers() []RegistryServer {
	return []RegistryServer{
		{
			Name:        "filesystem",
			Description: "File system operations for secure file access - UPDATED",
			Tier:        "Official",
			Status:      "Active",
			Transport:   "stdio",
			Tools:       []string{"read_file", "write_file", "list_directory"},
			Image:       "ghcr.io/modelcontextprotocol/server-filesystem:v2.0.0",
			Tags:        []string{"filesystem", "files", "v2"},
		},
		{
			Name:        "github",
			Description: "GitHub API integration",
			Tier:        "Official",
			Status:      "Active",
			Transport:   "stdio",
			Tools:       []string{"create_issue", "list_repos"},
			Image:       "ghcr.io/modelcontextprotocol/server-github:latest",
			Tags:        []string{"github", "vcs"},
		},
	}
}

// CreateComplexTestServers creates a complex set of test servers with full metadata
func CreateComplexTestServers() []RegistryServer {
	return []RegistryServer{
		{
			Name:        "filesystem",
			Description: "Advanced file system operations",
			Tier:        "Official",
			Status:      "Active",
			Transport:   "stdio",
			Tools:       []string{"read_file", "write_file", "list_directory", "search_files"},
			Image:       "ghcr.io/modelcontextprotocol/server-filesystem:v2.1.0",
			Tags:        []string{"filesystem", "files", "storage"},
			Metadata: map[string]string{
				"maintainer": "MCP Team",
				"license":    "MIT",
			},
		},
		{
			Name:        "github",
			Description: "Complete GitHub API integration",
			Tier:        "Official",
			Status:      "Active",
			Transport:   "stdio",
			Tools:       []string{"create_issue", "list_repos", "create_pr", "list_issues"},
			Image:       "ghcr.io/modelcontextprotocol/server-github:v1.5.0",
			Tags:        []string{"github", "vcs", "collaboration"},
			Metadata: map[string]string{
				"maintainer": "GitHub Team",
				"license":    "Apache-2.0",
			},
		},
	}
}

// ServersToMap converts a slice of servers to a map keyed by name
func ServersToMap(servers []RegistryServer) map[string]RegistryServer {
	result := make(map[string]RegistryServer)
	for _, server := range servers {
		result[server.Name] = server
	}
	return result
}
