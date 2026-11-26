package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/dig"
)

type greeting string

func newGreeting() greeting      { return "hello" }
func newOtherGreeting() greeting { return "hi" }

type farewell string

func newFarewell() farewell { return "bye" }

func TestProvide_RegistersEachConstructor(t *testing.T) {
	container := dig.New()

	require.NoError(t, Provide(container, newGreeting, newFarewell))

	require.NoError(t, container.Invoke(func(g greeting, f farewell) {
		assert.Equal(t, greeting("hello"), g)
		assert.Equal(t, farewell("bye"), f)
	}))
}

// A second constructor of a type used to be dropped without a word, and the first one won
func TestProvide_FailsNamingTheConstructorOfATypeAlreadyProvided(t *testing.T) {
	container := dig.New()
	require.NoError(t, Provide(container, newGreeting))

	err := Provide(container, newOtherGreeting, newFarewell)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to provide peekaping/internal/utils.newOtherGreeting:")
	assert.Error(t, container.Invoke(func(farewell) {}), "the constructors after the failed one are not provided")
}

func TestProvide_FailsNamingAValueThatIsNoConstructor(t *testing.T) {
	err := Provide(dig.New(), greeting("hello"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to provide utils.greeting:")
}
