package provider

import (
	"context"
	"errors"
	"strings"
)

// Provider defines the interface for communicating with LLMs
type Provider interface {
	GetCodeEdit(ctx context.Context, task string, fileName string, codeContent string, buildOutput string) (string, error)
}

// MockProvider is a rule-based mock LLM simulator for offline testing.
// It detects specific bugs in our demo files and returns pre-defined fixes,
// simulating the iterative Actor feedback loop.
type MockProvider struct{}

func NewMockProvider() *MockProvider {
	return &MockProvider{}
}

func (m *MockProvider) GetCodeEdit(ctx context.Context, task string, fileName string, codeContent string, buildOutput string) (string, error) {
	// Simulate thinking latency
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	// Rule 1: Detect math_utils.go bug
	if strings.Contains(fileName, "math_utils.go") {
		// If the error output indicates division by zero or unused imports from iteration 1, return the fix
		if strings.Contains(buildOutput, "divide by zero") || strings.Contains(buildOutput, "panic") || strings.Contains(buildOutput, "imported and not used") {
			fixedCode := `package mathutils

import "errors"

// Divide performs safe division.
func Divide(a, b int) (int, error) {
	if b == 0 {
		return 0, errors.New("cannot divide by zero")
	}
	return a / b, nil
}
`
			return fixedCode, nil
		}

		// Initial request or general fix
		if strings.Contains(codeContent, "return a / b") {
			// Returns a semi-fixed version that still panics if b is zero, simulating a bad first iteration
			semiFixed := `package mathutils

import "errors"

// Divide performs safe division.
func Divide(a, b int) (int, error) {
	// Todo: handle zero check
	return a / b, nil
}
`
			return semiFixed, nil
		}
	}

	return "", errors.New("mock provider: unsupported file or no matching rules found")
}
