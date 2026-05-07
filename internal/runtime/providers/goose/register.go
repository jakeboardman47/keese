// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package goose

import (
	spi "github.com/keese-ai/keese/internal/runtime/spi/v1alpha1"
)

func init() {
	spi.Register(ProviderName, capabilities, Factory)
}
