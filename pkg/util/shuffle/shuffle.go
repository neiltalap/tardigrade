// Copyright 2016 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package shuffle

import "math/rand"

// Interface for shuffle. When it is satisfied, a collection can be shuffled by
// the routines in this package. The methods require that the elements of the
// collection be enumerable by an integer index. This interface is similar to
// sort.Interface.
type Interface interface {
	// Len is the number of elements in the collection.
	Len() int
	// Swap swaps the elements with indexes i and j.
	Swap(i, j int)
}

// Shuffle randomizes the order of the array, using the default source of
// randomness.
func Shuffle(data Interface) {
	shuffle(data, rand.Intn)
}

// ShuffleWithRand randomizes the order of the array, using rng as the
// source of randomness. It exists so tests can supply a deterministic
// generator: as of Go 1.24 math/rand.Seed is a no-op, so exact-output
// tests can no longer seed the global generator.
func ShuffleWithRand(data Interface, rng *rand.Rand) {
	shuffle(data, rng.Intn)
}

func shuffle(data Interface, intn func(int) int) {
	n := data.Len()
	for i := 1; i < n; i++ {
		data.Swap(i, intn(i+1))
	}
}
