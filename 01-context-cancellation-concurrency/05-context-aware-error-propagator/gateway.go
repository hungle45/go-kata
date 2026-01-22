package main

import (
	"fmt"
	"log"
	"strings"
)

type Gateway struct {
	authService     AuthService
	metadataService MetadataService
}

func NewGateway(authService AuthService, metadataService MetadataService) *Gateway {
	return &Gateway{
		authService:     authService,
		metadataService: metadataService,
	}
}

func (g *Gateway) UploadFile(userID, apiKeys, filePath string) error {
	if err := g.authService.Authenticate(userID, apiKeys); err != nil {
		log.Default().Println(err.Error())
		return WrapError(err, "Gateway.UploadFile", "authentication failed")
	}
	if err := g.metadataService.CreateMetadata(filePath); err != nil {
		log.Default().Println(err.Error())
		return WrapError(err, "Gateway.UploadFile", "metadata creation failed")
	}
	return nil
}

//go:generate mockgen -destination=./mocks/mock_auth_service.go -package=mocks . AuthService
type AuthService interface {
	Authenticate(userID, apiKey string) error
}

//go:generate mockgen -destination=./mocks/mock_metadata_service.go -package=mocks . MetadataService
type MetadataService interface {
	CreateMetadata(filePath string) error
}

// ErrWrapper wraps an inner error.
type ErrWrapper struct {
	inner error
	err   error
}

func WrapError(err error, msg ...string) *ErrWrapper {
	return &ErrWrapper{
		inner: err,
		err:   fmt.Errorf("%s: %w", strings.Join(msg, ": "), err),
	}
}

func (e *ErrWrapper) Unwrap() error {
	return e.inner
}

func (e *ErrWrapper) Error() string {
	return e.err.Error()
}

type AuthErr struct {
	userID string
	err    error
	kind   ErrKind
}

func NewAuthErr(userID string, kind ErrKind, err error) *AuthErr {
	return &AuthErr{
		userID: userID,
		kind:   kind,
		err:    err,
	}
}

func (e *AuthErr) Error() string {
	if e.err != nil {
		return "authentication failed for user " + e.userID + ": " + e.err.Error()
	}
	return "authentication failed for user " + e.userID
}

func (e *AuthErr) Unwrap() error {
	return e.err
}

func (e *AuthErr) Timeout() bool {
	return e.kind == TimeoutErrKind
}

func (e *AuthErr) Temporary() bool {
	return e.kind == TemporaryErrKind
}

type StorageErr struct {
	filePath string
	kind     ErrKind
	err      error
}

func NewStorageErr(filePath string, kind ErrKind, err error) *StorageErr {
	return &StorageErr{
		filePath: filePath,
		kind:     kind,
		err:      err,
	}
}

func (e *StorageErr) Error() string {
	if e.err != nil {
		return "storage failed for file " + e.filePath + ": " + e.err.Error()
	}
	return "storage failed for file " + e.filePath
}

func (e *StorageErr) Unwrap() error {
	return e.err
}

func (e *StorageErr) Timeout() bool {
	return e.kind == TimeoutErrKind
}

func (e *StorageErr) Temporary() bool {
	return e.kind == TemporaryErrKind
}

type ErrKind int

const (
	TimeoutErrKind ErrKind = iota
	TemporaryErrKind
)

// Temporary interface indicates whether an error is temporary.
type Temporary interface {
	Temporary() bool
}

func IsTemporary(err error) bool {
	te, ok := err.(Temporary)
	return ok && te.Temporary()
}

// Timeout interface indicates whether an error is due to a timeout.
type Timeout interface {
	Timeout() bool
}

func IsTimeout(err error) bool {
	to, ok := err.(Timeout)
	return ok && to.Timeout()
}
