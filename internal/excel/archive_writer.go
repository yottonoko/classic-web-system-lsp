package excel

import (
	"archive/zip"
	"errors"
	"io"
	"io/fs"
	"math"
	"os"

	"github.com/xuri/excelize/v2"
)

// ErrWorkbookTooLarge reports that direct XLSX output exceeded the ordinary
// non-ZIP64 workbook limit.
var ErrWorkbookTooLarge = errors.New("excel workbook exceeds the 4 GiB direct-write limit")

func writeWorkbookArchiveFile(file *excelize.File, path string, configuredLimit int64) (err error) {
	limit := configuredLimit
	if limit <= 0 || limit > math.MaxUint32 {
		limit = math.MaxUint32
	}
	output, err := os.Create(path)
	if err != nil {
		return err
	}
	succeeded := false
	defer func() {
		closeErr := output.Close()
		if err == nil && closeErr != nil {
			err = closeErr
		}
		if !succeeded || err != nil {
			_ = os.Remove(path)
		}
	}()

	limitedOutput := &archiveLimitWriter{writer: output, remaining: limit}
	previousPath := file.Path
	previousZipWriter := file.ZipWriter
	file.Path = path
	file.ZipWriter = func(io.Writer) excelize.ZipWriter {
		return &limitedZipWriter{writer: zip.NewWriter(limitedOutput), entryLimit: limit}
	}
	defer func() {
		file.Path = previousPath
		file.ZipWriter = previousZipWriter
	}()

	if _, err = file.WriteTo(io.Discard); err != nil {
		return err
	}
	if err = output.Sync(); err != nil {
		return err
	}
	succeeded = true
	return nil
}

type archiveLimitWriter struct {
	writer    io.Writer
	remaining int64
}

func (writer *archiveLimitWriter) Write(value []byte) (int, error) {
	if int64(len(value)) > writer.remaining {
		return 0, ErrWorkbookTooLarge
	}
	written, err := writer.writer.Write(value)
	writer.remaining -= int64(written)
	return written, err
}

type limitedZipWriter struct {
	writer     *zip.Writer
	entryLimit int64
}

func (writer *limitedZipWriter) Create(name string) (io.Writer, error) {
	entry, err := writer.writer.Create(name)
	if err != nil {
		return nil, err
	}
	return &archiveLimitWriter{writer: entry, remaining: writer.entryLimit}, nil
}

func (writer *limitedZipWriter) AddFS(fs.FS) error {
	return errors.New("direct XLSX writer does not support AddFS")
}

func (writer *limitedZipWriter) Close() error {
	return writer.writer.Close()
}
