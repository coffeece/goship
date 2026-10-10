package deploy

// Target is the organization and project a command acts on.
type Target struct{ Org, Project string }

func (t Target) String() string { return t.Org + "/" + t.Project }
