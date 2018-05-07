package utils

import (
	"github.com/stretchr/testify/assert"
	"os/user"
	"testing"
)

func TestHomeDirectory(t *testing.T) {
	usr, err := user.Current()
	assert.NoError(t, err)

	home := HomeDir()
	assert.Equal(t, home, usr.HomeDir)
}
