package oursql

/*
#include "oursql.h"
*/
import "C"
import (
	"fmt"
	"unsafe"
)

func init() {
	// This needs to be called before threads begin to spawn.
	C.our_library_init()
}

type ConnectionParams struct {
	Host       string
	Port       int
	Uname      string
	Pass       string
	DbName     string
	UnixSocket string
	Charset    string
	Flags      uint64
}

func (c *ConnectionParams) EnableMultiStatements() {
	c.Flags |= C.CLIENT_MULTI_STATEMENTS
}

func (c *ConnectionParams) Redact() {
	c.Pass = "***"
}

type SqlError struct {
	Num     int
	Message string
	Query   string
}

func (se *SqlError) Error() string {
	if se.Query == "" {
		return fmt.Sprintf("%v (errno %v)", se.Message, se.Num)
	}
	return fmt.Sprintf("%v (errno %v) during query: %s", se.Message, se.Num, se.Query)
}

func (se *SqlError) Number() int {
	return se.Num
}

type Connection struct {
	c      C.MYSQL
	closed bool
}

func Connect(params ConnectionParams) (conn *Connection, err error) {
	host := C.CString(params.Host)
	defer cfree(host)
	port := C.uint(params.Port)
	uname := C.CString(params.Uname)
	defer cfree(uname)
	pass := C.CString(params.Pass)
	defer cfree(pass)
	dbname := C.CString(params.DbName)
	defer cfree(dbname)
	unix_socket := C.CString(params.UnixSocket)
	defer cfree(unix_socket)
	charset := C.CString(params.Charset)
	defer cfree(charset)
	flags := C.ulong(params.Flags)

	conn = &Connection{}
	if C.our_connect(&conn.c, host, uname, pass, dbname, port, unix_socket, charset, flags) != 0 {
		defer conn.Close()
		return nil, conn.lastError("")
	}
	return conn, nil
}

func (conn *Connection) lastError(query string) error {
	if err := C.our_error(&conn.c); *err != 0 {
		return &SqlError{Num: int(C.our_errno(&conn.c)), Message: C.GoString(err), Query: query}
	}
	return &SqlError{0, "Unknow", string(query)}
}

func (conn *Connection) Id() int64 {
	return int64(C.our_thread_id(&conn.c))
}

func (conn *Connection) Close() {
	C.our_close(&conn.c)
	conn.closed = true
}

func (conn *Connection) IsClosed() bool {
	return conn.closed
}

func (conn *Connection) execute(sql string, mode C.OUR_MODE, res *Result) error {
	if conn.IsClosed() {
		return &SqlError{Num: 2006, Message: "Connection is closed"}
	}

	if C.our_query(&conn.c, &res.c, (*C.char)(stringPointer(sql)), C.ulong(len(sql)), mode) != 0 {
		return conn.lastError(sql)
	}

	res.conn = conn
	res.RowsAffected = uint64(conn.c.affected_rows)
	res.InsertId = uint64(conn.c.insert_id)

	return nil
}

func (conn *Connection) query(sql string, mode C.OUR_MODE, res *_QueryResult, maxrows int, wantFields bool) error {
	err := conn.execute(sql, mode, &res.Result)
	if err != nil {
		return err
	}

	if res.c.num_fields == 0 {
		return nil
	}

	if maxrows > 0 && res.RowsAffected > uint64(maxrows) {
		return &SqlError{0, fmt.Sprintf("Row count exceeded %d", maxrows), string(sql)}
	}

	if wantFields {
		res.fillFields()
	}

	return nil
}

func (conn *Connection) Execute(sql string) (res *Result, err error) {
	res = &Result{}

	err = conn.execute(sql, C.OUR_MODE_NON, res)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func (conn *Connection) QuerySet(sql string, maxrows int, wantFields bool) (res *DataSet, err error) {
	res = &DataSet{}

	err = conn.query(sql, C.OUR_MODE_SET, &res._QueryResult, maxrows, wantFields)
	if err != nil {
		return nil, err
	}
	defer res.close()

	err = res.fillRows()
	if err != nil {
		return nil, err
	}

	return res, nil
}

func (conn *Connection) QueryReader(sql string, maxrows int, wantFields bool) (res *DataReader, err error) {
	res = &DataReader{}

	err = conn.query(sql, C.OUR_MODE_READER, &res._QueryResult, maxrows, wantFields)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func cfree(str *C.char) {
	if str != nil {
		C.free(unsafe.Pointer(str))
	}
}
