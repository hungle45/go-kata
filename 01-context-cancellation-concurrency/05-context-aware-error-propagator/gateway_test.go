package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"context-aware-error-propagator/mocks"

	"go.uber.org/mock/gomock"
)

// Test 1: The "Sensitive Data Leak"
func TestSensitiveDataLeak(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockAuth := mocks.NewMockAuthService(ctrl)
	mockMetadata := mocks.NewMockMetadataService(ctrl)

	apiKey := "super_secret_key_12345"
	authErr := NewAuthErr("user123", TimeoutErrKind, errors.New("invalid credentials"))

	mockAuth.EXPECT().Authenticate("user123", apiKey).Return(authErr)

	gateway := NewGateway(mockAuth, mockMetadata)
	err := gateway.UploadFile("user123", apiKey, "/test/file.txt")

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// The error string should NOT contain the API key
	errString := fmt.Sprint(err)
	if strings.Contains(errString, apiKey) {
		t.Errorf("FAIL: Error message leaked sensitive API key: %s", errString)
	}
}

// Test 2: The "Lost Context"
func TestLostContext(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockAuth := mocks.NewMockAuthService(ctrl)
	mockMetadata := mocks.NewMockMetadataService(ctrl)

	// Create an AuthErr and wrap it three times
	authErr := NewAuthErr("user456", TemporaryErrKind, errors.New("network error"))
	wrappedOnce := WrapError(authErr, "layer1", "first wrap")
	wrappedTwice := WrapError(wrappedOnce, "layer2", "second wrap")
	wrappedThrice := WrapError(wrappedTwice, "layer3", "third wrap")

	mockAuth.EXPECT().Authenticate(gomock.Any(), gomock.Any()).Return(wrappedThrice)

	gateway := NewGateway(mockAuth, mockMetadata)
	err := gateway.UploadFile("user456", "key789", "/test/file.txt")

	// errors.As should be able to extract the original AuthErr
	var targetErr *AuthErr
	if !errors.As(err, &targetErr) {
		t.Error("FAIL: errors.As() could not unwrap to AuthErr - context was lost")
	}
}

// Test 3: The "Timeout Confusion"
func TestTimeoutConfusion(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockAuth := mocks.NewMockAuthService(ctrl)
	mockMetadata := mocks.NewMockMetadataService(ctrl)

	// Create a storage timeout error that wraps context.DeadlineExceeded
	storageErr := NewStorageErr("/big/file.bin", TimeoutErrKind, context.DeadlineExceeded)
	wrappedErr := WrapError(storageErr, "storage layer")

	mockAuth.EXPECT().Authenticate(gomock.Any(), gomock.Any()).Return(nil)
	mockMetadata.EXPECT().CreateMetadata(gomock.Any()).Return(wrappedErr)

	gateway := NewGateway(mockAuth, mockMetadata)
	err := gateway.UploadFile("user789", "key123", "/big/file.bin")

	// errors.Is should recognize context.DeadlineExceeded
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Error("FAIL: errors.Is(err, context.DeadlineExceeded) returned false - timeout context was lost")
	}
}
