package render

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"text/tabwriter"
)

const (
	Table = "table"
	JSON  = "json"
)

// Renderer is the only thing that writes command results to stdout. Commands
// return values; the format is decided here, which is what makes --output json
// work everywhere instead of command by command.
type Renderer struct {
	out    io.Writer
	format string
}

func New(out io.Writer, format string) *Renderer {
	return &Renderer{out: out, format: format}
}

// Render writes v as a table or as JSON. For tables, v must be a struct or a
// slice of structs; the columns are the fields tagged `table:"HEADER"`, in
// declaration order.
func (r *Renderer) Render(v any) error {
	if r.format == JSON {
		enc := json.NewEncoder(r.out)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	return r.table(v)
}

// Message reports an outcome that carries no data of its own.
func (r *Renderer) Message(format string, args ...any) error {
	if r.format == JSON {
		return json.NewEncoder(r.out).Encode(map[string]string{"message": fmt.Sprintf(format, args...)})
	}
	_, err := fmt.Fprintf(r.out, format+"\n", args...)
	return err
}

func (r *Renderer) table(v any) error {
	rv := reflect.Indirect(reflect.ValueOf(v))

	rows := []reflect.Value{rv}
	if rv.Kind() == reflect.Slice {
		rows = nil
		for i := 0; i < rv.Len(); i++ {
			rows = append(rows, reflect.Indirect(rv.Index(i)))
		}
	}
	if len(rows) == 0 {
		_, err := fmt.Fprintln(r.out, "No results.")
		return err
	}

	cols := columns(rows[0].Type())
	if len(cols) == 0 {
		return fmt.Errorf("render: %s has no table-tagged fields", rows[0].Type())
	}

	w := tabwriter.NewWriter(r.out, 0, 0, 3, ' ', 0)
	headers := make([]string, len(cols))
	for i, c := range cols {
		headers[i] = c.header
	}
	fmt.Fprintln(w, strings.Join(headers, "\t"))

	for _, row := range rows {
		cells := make([]string, len(cols))
		for i, c := range cols {
			cells[i] = cell(row.Field(c.index))
		}
		fmt.Fprintln(w, strings.Join(cells, "\t"))
	}
	return w.Flush()
}

type column struct {
	index  int
	header string
}

func columns(t reflect.Type) []column {
	if t.Kind() != reflect.Struct {
		return nil
	}
	var cols []column
	for i := 0; i < t.NumField(); i++ {
		if tag := t.Field(i).Tag.Get("table"); tag != "" && tag != "-" {
			cols = append(cols, column{index: i, header: tag})
		}
	}
	return cols
}

func cell(v reflect.Value) string {
	if v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return "-"
		}
		v = v.Elem()
	}
	if v.Kind() == reflect.Slice {
		parts := make([]string, v.Len())
		for i := range parts {
			parts[i] = cell(v.Index(i))
		}
		if len(parts) == 0 {
			return "-"
		}
		return strings.Join(parts, ",")
	}
	s := fmt.Sprintf("%v", v.Interface())
	if s == "" {
		return "-"
	}
	return s
}
