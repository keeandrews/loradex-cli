package project

import (
	"path/filepath"

	"github.com/keeandrews/loradex-cli/internal/transfer"
)

// Status describes local-vs-remote drift for a project.
type Status struct {
	Owner          string `json:"owner"`
	Repo           string `json:"repo"`
	Endpoint       string `json:"endpoint"`
	WeightsPath    string `json:"weights"`
	LocalSHA256    string `json:"local_sha256"`
	LastPushTag    string `json:"last_push_version"`
	LastPushSHA256 string `json:"last_push_sha256"`
	RemoteLatest   string `json:"remote_latest"`
	RemoteChecked  bool   `json:"remote_checked"`
	WeightsChanged bool   `json:"weights_changed"`
	UpToDate       bool   `json:"up_to_date"`
	Message        string `json:"message"`
}

// Compute hashes the local weights and compares them to the last-push record
// and (optionally) the remote latest version.
func Compute(p *Project, remoteLatest string, remoteChecked bool) (*Status, error) {
	weights := filepath.Join(p.Dir, p.Catalog.Weights)
	localSHA, _, err := transfer.HashFile(weights)
	if err != nil {
		return nil, err
	}
	s := &Status{
		Owner:         p.Config.Owner,
		Repo:          p.Config.Repo,
		Endpoint:      p.Config.Endpoint,
		WeightsPath:   p.Catalog.Weights,
		LocalSHA256:   localSHA,
		RemoteLatest:  remoteLatest,
		RemoteChecked: remoteChecked,
	}
	if p.Config.LastPush != nil {
		s.LastPushTag = p.Config.LastPush.Version
		s.LastPushSHA256 = p.Config.LastPush.SHA256
	}

	switch {
	case s.LastPushSHA256 == "":
		s.WeightsChanged = true
		s.Message = "not pushed yet — `loradex push` will create the first version"
	case localSHA != s.LastPushSHA256:
		s.WeightsChanged = true
		s.Message = "local weights changed — next push will create a new version"
	default:
		s.UpToDate = true
		s.Message = "up to date with " + s.LastPushTag
	}
	if remoteChecked && remoteLatest != "" && s.LastPushTag != "" && remoteLatest != s.LastPushTag {
		s.Message += " · remote latest is " + remoteLatest
	}
	if !remoteChecked {
		s.Message += " (remote not checked)"
	}
	return s, nil
}
