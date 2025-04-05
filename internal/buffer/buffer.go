package buffer

type Buffer[T any] struct {
	b []T
}

func NewBuffer[T any]() *Buffer[T] {
	return &Buffer[T]{b: make([]T, 0)}
}

func (b *Buffer[T]) Flush() {
	if len(b.b) == 0 {
		return
	}
	b.b = b.b[:0]
}

func (b *Buffer[T]) Append(t T) {
	b.b = append(b.b, t)
}

func (b *Buffer[T]) Size() int {
	return len(b.b)
}

func (b *Buffer[T]) Load() []T {
	return b.b
}
