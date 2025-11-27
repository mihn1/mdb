package sstable

type TableError struct {
	msg      string
	fileName string
}

func (e *TableError) Error() string {
	if e.fileName != "" {
		return e.msg + " (file: " + e.fileName + ")"
	}
	return e.msg
}

func newTableErrorMsg(msg string) error {
	return &TableError{
		msg: msg,
	}
}

func newTableError(msg, fileName string) error {
	return &TableError{
		msg:      msg,
		fileName: fileName,
	}
}

var ErrInvalidFooter = newTableErrorMsg("invalid footer")
var ErrInvalidBlockMeta = newTableErrorMsg("invalid block meta")
var ErrInvalidBlock = newTableErrorMsg("invalid block")
var ErrInvalidBlockReader = newTableErrorMsg("invalid block reader")
var ErrInvalidIndex = newTableErrorMsg("invalid index")
var ErrInvalidFilterBlock = newTableErrorMsg("invalid filter block")
var ErrEmptyTable = newTableErrorMsg("empty table")
