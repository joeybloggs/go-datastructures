/*
Copyright 2014 Workiva, LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

 http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package rangetree

import "github.com/Workiva/go-datastructures/slice"

type ImmutableRangeTree struct {
	number     uint64
	top        orderedNodes
	dimensions uint64
}

func newCache(dimensions uint64) []slice.Int64Slice {
	cache := make([]slice.Int64Slice, 0, dimensions-1)
	for i := uint64(0); i < dimensions; i++ {
		cache = append(cache, slice.Int64Slice{})
	}
	return cache
}

func (irt *ImmutableRangeTree) needNextDimension() bool {
	return irt.dimensions > 1
}

func (irt *ImmutableRangeTree) add(nodes *orderedNodes, cache []slice.Int64Slice, entry Entry, added *uint64) {
	var node *node
	list := nodes

	for i := uint64(1); i <= irt.dimensions; i++ {
		if isLastDimension(irt.dimensions, i) {
			if i != 1 && !cache[i-1].Exists(node.value) {
				nodes := make(orderedNodes, len(*list))
				copy(nodes, *list)
				list = &nodes
				cache[i-1].Insert(node.value)
			}

			newNode := newNode(entry.ValueAtDimension(i), entry, false)
			overwritten := list.add(newNode)
			if overwritten == nil {
				*added++
			}
			if node != nil {
				node.orderedNodes = *list
			}
			break
		}

		if i != 1 && !cache[i-1].Exists(node.value) {
			nodes := make(orderedNodes, len(*list))
			copy(nodes, *list)
			list = &nodes
			cache[i-1].Insert(node.value)
			node.orderedNodes = *list
		}

		node, _ = list.getOrAdd(entry, i, irt.dimensions)
		list = &node.orderedNodes
	}
}

// Add will add the provided entries into the tree and return
// a new tree with those entries added.
func (irt *ImmutableRangeTree) Add(entries ...Entry) *ImmutableRangeTree {
	if len(entries) == 0 {
		return irt
	}

	cache := newCache(irt.dimensions)
	top := make(orderedNodes, len(irt.top))
	copy(top, irt.top)
	added := uint64(0)
	for _, entry := range entries {
		irt.add(&top, cache, entry, &added)
	}

	tree := NewImmutableRangeTree(irt.dimensions)
	tree.top = top
	tree.number = irt.number + added
	return tree
}

// InsertAtDimension will increment items at and above the given index
// by the number provided.  Provide a negative number to to decrement.
// Returned are two lists and the modified tree.  The first list is a
// list of entries that were moved.  The second is a list entries that
// were deleted.  These lists are exclusive.
func (irt *ImmutableRangeTree) InsertAtDimension(dimension uint64,
	index, number int64) (*ImmutableRangeTree, Entries, Entries) {

	if dimension > irt.dimensions || number == 0 {
		return irt, nil, nil
	}

	modified, deleted := make(Entries, 0, 100), make(Entries, 0, 100)

	tree := NewImmutableRangeTree(irt.dimensions)
	tree.top = irt.top.immutableInsert(
		dimension, 1, irt.dimensions,
		index, number,
		&modified, &deleted,
	)
	tree.number = irt.number - uint64(len(deleted))

	return tree, modified, deleted
}

type immutableNodeBundle struct {
	list         *orderedNodes
	index        int
	previousNode *node
	newNode      *node
}

func (irt *ImmutableRangeTree) Delete(entries ...Entry) *ImmutableRangeTree {
	cache := newCache(irt.dimensions)
	top := make(orderedNodes, len(irt.top))
	copy(top, irt.top)
	deleted := uint64(0)
	for _, entry := range entries {
		irt.delete(&top, cache, entry, &deleted)
	}

	tree := NewImmutableRangeTree(irt.dimensions)
	tree.top = top
	tree.number = irt.number - deleted
	return tree
}

func (irt *ImmutableRangeTree) delete(top *orderedNodes,
	cache []slice.Int64Slice, entry Entry, deleted *uint64) {

	path := make([]*immutableNodeBundle, 0, 5)
	var index int
	var n *node
	var local *node
	list := top

	for i := uint64(1); i <= irt.dimensions; i++ {
		value := entry.ValueAtDimension(i)
		local, index = list.get(value)
		if local == nil { // there's nothing to delete
			return
		}

		nb := &immutableNodeBundle{
			list:         list,
			index:        index,
			previousNode: n,
		}
		path = append(path, nb)
		n = local
		list = &n.orderedNodes
	}

	*deleted++

	for i := len(path) - 1; i >= 0; i-- {
		nb := path[i]
		if nb.previousNode != nil {
			nodes := make(orderedNodes, len(*nb.list))
			copy(nodes, *nb.list)
			nb.list = &nodes
			if len(*nb.list) == 1 {
				continue
			}
			nn := newNode(
				nb.previousNode.value,
				nb.previousNode.entry,
				!isLastDimension(irt.dimensions, uint64(i)+1),
			)
			nn.orderedNodes = nodes
			path[i-1].newNode = nn
		}
	}

	for _, nb := range path {
		if nb.newNode == nil {
			nb.list.deleteAt(nb.index)
		} else {
			(*nb.list)[nb.index] = nb.newNode
		}
	}
}

func (irt *ImmutableRangeTree) apply(list orderedNodes, interval Interval,
	dimension uint64, fn func(*node) bool) bool {

	low, high := interval.LowAtDimension(dimension), interval.HighAtDimension(dimension)

	if isLastDimension(irt.dimensions, dimension) {
		if !list.apply(low, high, fn) {
			return false
		}
	} else {
		if !list.apply(low, high, func(n *node) bool {
			if !irt.apply(n.orderedNodes, interval, dimension+1, fn) {
				return false
			}
			return true
		}) {
			return false
		}
		return true
	}

	return true
}

// Query will return an ordered list of results in the given
// interval.
func (irt *ImmutableRangeTree) Query(interval Interval) Entries {
	entries := NewEntries()

	irt.apply(irt.top, interval, 1, func(n *node) bool {
		entries = append(entries, n.entry)
		return true
	})

	return entries
}

// Len returns the number of items in this tree.
func (irt *ImmutableRangeTree) Len() uint64 {
	return irt.number
}

func NewImmutableRangeTree(dimensions uint64) *ImmutableRangeTree {
	return &ImmutableRangeTree{
		dimensions: dimensions,
	}
}
