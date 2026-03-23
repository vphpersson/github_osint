package repository

type Owner struct {
	Login string `json:"login"`
}

type Repository struct {
	Name      string `json:"name"`
	FullName  string `json:"full_name"`
	Fork      bool   `json:"fork"`
	CreatedAt string `json:"created_at"`
	Owner     *Owner `json:"owner"`
}
