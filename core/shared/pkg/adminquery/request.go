package adminquery

type Request struct {
	Filter   string
	OrderBy  string
	Offset   *int32
	PageSize int32
}

func (r Request) Paged() bool {
	return r.Offset != nil
}

func (r Request) Page() (int, int) {
	var offset int32
	if r.Offset != nil {
		offset = *r.Offset
	}
	return Page(r.PageSize, offset)
}

func (s *Schema) Prepare(r Request, w *Where) (Query, error) {
	q, err := s.Compile(r.Filter, r.OrderBy)
	if err != nil {
		return Query{}, err
	}
	if !r.Paged() && r.OrderBy != "" {
		return Query{}, &Error{Field: "order_by", Message: "needs offset paging"}
	}
	w.AddQuery(q)
	return q, nil
}
