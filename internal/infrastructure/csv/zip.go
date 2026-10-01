package csv

import (
	"archive/zip"
	"io"
	"time"

	"github.com/le-marais/claimsgen/internal/application"
)

// zipModified is the timestamp every archive entry carries: fixed, so equal
// runs give byte-identical archives, and the earliest date a zip can hold.
var zipModified = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)

// WriteZip writes all five CSVs into one zip archive on w: the dataset
// files, then the aggregate files.
func WriteZip(w io.Writer, ds application.Dataset, ag application.Aggregates) error {
	z := zip.NewWriter(w)
	open := func(name string) (io.Writer, func() error, error) {
		f, err := z.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: zipModified})
		return f, func() error { return nil }, err
	}
	if err := writeDataset(open, ds); err != nil {
		return err
	}
	if err := writeAggregates(open, ag); err != nil {
		return err
	}
	return z.Close()
}
