package commit

type Author struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type CommitData struct {
	Author *Author `json:"author"`
}

type Commit struct {
	Commit *CommitData `json:"commit"`
}
