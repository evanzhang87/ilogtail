package helper

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"path"
	"sync"
	"time"

	"github.com/alibaba/ilogtail/pkg/logger"
	"github.com/alibaba/ilogtail/pkg/util"
)

type DumpDataReq struct {
	Body   []byte
	URL    string
	Header map[string][]string
}
type DumpDataResp struct {
	Body   []byte
	Header map[string]string
}

// DumpData current only for http protocol
type DumpData struct {
	Req  DumpDataReq
	Resp DumpDataResp
}

type Dumper struct {
	input             chan *DumpData
	dumpDataKeepFiles []string
	prefix            string
	maxKeepFiles      int
	wg                sync.WaitGroup
	stop              chan struct{}
}

func (d *Dumper) Init() {
	_ = os.MkdirAll(path.Join(util.GetCurrentBinaryPath(), "dump"), 0750)
	d.input = make(chan *DumpData, 10)
	d.stop = make(chan struct{})
	files, err := GetFileListByPrefix(path.Join(util.GetCurrentBinaryPath(), "dump"), d.prefix, true, 0)
	if err != nil {
		logger.Warning(context.Background(), "LIST_HISTORY_DUMP_ALARM", "err", err)
	} else {
		d.dumpDataKeepFiles = files
	}

}

func (d *Dumper) Start() {
	if d == nil {
		return
	}
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.doDumpFile()
	}()
}

func (d *Dumper) Close() {
	close(d.stop)
	d.wg.Wait()
}

func (d *Dumper) doDumpFile() {
	var err error
	fileName := d.prefix + ".dump"
	logger.Info(context.Background(), "http server start dump data", fileName)
	var f *os.File
	closeFile := func() {
		if f != nil {
			_ = f.Close()
		}
	}
	cutFile := func() (f *os.File, err error) {
		nFile := path.Join(path.Join(util.GetCurrentBinaryPath(), "dump"), fileName+"_"+time.Now().Format("2006-01-02_15"))
		if len(d.dumpDataKeepFiles) == 0 || d.dumpDataKeepFiles[len(d.dumpDataKeepFiles)-1] != nFile {
			d.dumpDataKeepFiles = append(d.dumpDataKeepFiles, nFile)
		}
		if len(d.dumpDataKeepFiles) > d.maxKeepFiles {
			rmLen := len(d.dumpDataKeepFiles) - d.maxKeepFiles
			for i := 0; i < rmLen; i++ {
				_ = os.Remove(d.dumpDataKeepFiles[i])
			}
			d.dumpDataKeepFiles = d.dumpDataKeepFiles[rmLen:]
		}
		closeFile()
		return os.OpenFile(d.dumpDataKeepFiles[len(d.dumpDataKeepFiles)-1], os.O_CREATE|os.O_WRONLY, 0600)
	}
	lastHour := 0
	offset := int64(0)
	for {
		select {
		case data := <-d.input:
			if time.Now().Hour() != lastHour {
				file, cerr := cutFile()
				if cerr != nil {
					logger.Error(context.Background(), "DUMP_FILE_ALARM", "cut new file error", err)
				} else {
					offset, _ = file.Seek(0, io.SeekEnd)
					f = file
				}
			}
			if f != nil {
				buffer := bytes.NewBuffer([]byte{})
				b, _ := json.Marshal(data)
				if err = binary.Write(buffer, binary.BigEndian, uint32(len(b))); err != nil {
					continue
				}
				if _, err = f.WriteAt(buffer.Bytes(), offset); err != nil {
					continue
				}
				if _, err = f.WriteAt(b, offset+4); err != nil {
					continue
				}
				offset += int64(4 + len(b))
			}
		case <-d.stop:
			return
		}
	}
}

func (d *Dumper) InputChannel() chan *DumpData {
	return d.input
}

func NewDumper(prefix string, maxFiles int) *Dumper {
	return &Dumper{
		prefix:       prefix,
		maxKeepFiles: maxFiles,
	}
}
