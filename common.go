package config

// ModifiedFile contains a file and its modify operation
type ModifiedFile struct {
	Op   string `json:"op,omitempty"`
	Path string `json:"path,omitempty"`
}

// CommitInfo contains git commit info
type CommitInfo struct {
	TimeStamp      int64  `json:"timestamp,omitempty"`
	CommitterEmail string `json:"committerEmail,omitempty"`
	Subject        string `json:"subject,omitempty"`
}

// ConfigInfo defines a configuration service information
type ConfigInfo struct {
	Repo     string         `json:"repo,omitempty"`
	Branch   string         `json:"branch,omitempty"`
	Version  string         `json:"version,omitempty"`
	Commit   CommitInfo     `json:"commit,omitempty"`
	ModFiles []ModifiedFile `json:"modfiles,omitempty"`
	// PathToRepo is the path to git root directory
	PathToRepo string `json:"path_to_repo,omitempty"`
}

// infoPrefix is the tree information file name
const infoPrefix = "%s/._info"
