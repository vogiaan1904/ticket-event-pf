package paginator

const (
	defaultLimit = 15
)

// PaginatorQuery is a struct for cursor-based pagination requests.
type PaginatorQuery struct {
	Limit  int64  `json:"limit" form:"limit"`
	Cursor string `json:"cursor" form:"cursor"` // Base64 encoded LastEvaluatedKey
}

// Adjust adjusts the paginator's limit to the default value if invalid.
func (p *PaginatorQuery) Adjust() {
	if p.Limit < 1 {
		p.Limit = defaultLimit
	}
}

// Paginator represents cursor-based pagination response.
type Paginator struct {
	Count      int64  `json:"count"`
	PageSize   int64  `json:"page_size"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

// ToResponse converts the paginator to a response.
func (p Paginator) ToResponse() PaginatorResponse {
	return PaginatorResponse{
		Count:      p.Count,
		PageSize:   p.PageSize,
		NextCursor: p.NextCursor,
		HasMore:    p.HasMore,
	}
}

// PaginatorResponse is a struct that contains the response of a paginator.
type PaginatorResponse struct {
	Count      int64  `json:"count"`
	PageSize   int64  `json:"page_size"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}
