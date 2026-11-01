// Copyright 2026 ALTESSA SOLUTIONS INC.
// Use of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package cloud_test

import (
	"testing"

	"github.com/altessa-s/rmcloud-go"

	"github.com/altessa-s/rmctl/internal/adapter/cloud"
	"github.com/altessa-s/rmctl/internal/clouddouble"
)

// registered is the adapter over a client the double already accepts.
func registered(t testing.TB) (*clouddouble.Cloud, *cloud.Cloud) {
	t.Helper()
	double := clouddouble.New(t)
	return double, cloud.New(double.Client(t, double.Registered(t)))
}

// unregistered is the adapter over a client with an empty store.
func unregistered(t testing.TB) (*clouddouble.Cloud, *cloud.Cloud, rmcloud.SecretStore) {
	t.Helper()
	double := clouddouble.New(t)
	store := clouddouble.NewStore()
	return double, cloud.New(double.Client(t, store)), store
}
