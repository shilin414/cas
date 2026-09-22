package execution

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
)

type admissionConnector struct{ options chan driver.TxOptions }

func (c *admissionConnector) Connect(context.Context) (driver.Conn, error) {
	return &admissionConnection{c.options}, nil
}
func (c *admissionConnector) Driver() driver.Driver { return admissionDriver{} }

type admissionDriver struct{}

func (admissionDriver) Open(string) (driver.Conn, error) { return nil, errors.New("unexpected open") }

type admissionConnection struct{ options chan driver.TxOptions }

func (*admissionConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected query")
}
func (*admissionConnection) Close() error { return nil }
func (*admissionConnection) Begin() (driver.Tx, error) {
	return nil, errors.New("default isolation must not be used")
}
func (c *admissionConnection) BeginTx(_ context.Context, opts driver.TxOptions) (driver.Tx, error) {
	c.options <- opts
	return admissionTx{}, nil
}

type admissionTx struct{}

func (admissionTx) Commit() error   { return nil }
func (admissionTx) Rollback() error { return nil }
func TestAdmissionTransactionAlwaysUsesReadCommitted(t *testing.T) {
	options := make(chan driver.TxOptions, 1)
	conn := sql.OpenDB(&admissionConnector{options})
	defer conn.Close()
	tx, err := BeginUserAdmissionTx(context.Background(), conn)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	opts := <-options
	if opts.Isolation != driver.IsolationLevel(sql.LevelReadCommitted) || opts.ReadOnly {
		t.Fatalf("unexpected isolation %+v", opts)
	}
}
