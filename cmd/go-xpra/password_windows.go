//go:build windows

package main

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

const (
	credUIFlagsDoNotPersist        = 0x00000002
	credUIFlagsExcludeCertificates = 0x00000008
	credUIFlagsAlwaysShowUI        = 0x00000080
	credUIFlagsGenericCredentials  = 0x00040000
	credUIFlagsKeepUsername        = 0x00100000
	errorCancelled                 = 1223
)

var credUIPromptForCredentials = syscall.NewLazyDLL("credui.dll").
	NewProc("CredUIPromptForCredentialsW")

type credUIInfo struct {
	size        uint32
	parent      uintptr
	messageText *uint16
	captionText *uint16
	banner      uintptr
}

func promptPassword(username, address, description string) (string, error) {
	caption, err := syscall.UTF16PtrFromString("Xpra authentication")
	if err != nil {
		return "", err
	}
	message := "Enter the password requested by Xpra server " + address + "."
	if description != "" && description != "password" {
		message += "\n" + description
	}
	messageText, err := syscall.UTF16PtrFromString(message)
	if err != nil {
		return "", err
	}
	target, err := syscall.UTF16PtrFromString("go-xpra/" + address)
	if err != nil {
		return "", err
	}

	var usernameBuffer [514]uint16
	usernameUTF16, err := syscall.UTF16FromString(username)
	if err != nil {
		return "", err
	}
	if len(usernameUTF16) > len(usernameBuffer) {
		return "", errors.New("user name is too long for the Windows credentials dialog")
	}
	copy(usernameBuffer[:], usernameUTF16)
	var passwordBuffer [256]uint16
	defer func() {
		for i := range passwordBuffer {
			passwordBuffer[i] = 0
		}
	}()

	info := credUIInfo{
		size:        uint32(unsafe.Sizeof(credUIInfo{})),
		messageText: messageText,
		captionText: caption,
	}
	flags := uintptr(credUIFlagsDoNotPersist | credUIFlagsExcludeCertificates |
		credUIFlagsAlwaysShowUI | credUIFlagsGenericCredentials)
	if username != "" {
		flags |= credUIFlagsKeepUsername
	}
	var save int32
	result, _, callErr := credUIPromptForCredentials.Call(
		uintptr(unsafe.Pointer(&info)),
		uintptr(unsafe.Pointer(target)),
		0,
		0,
		uintptr(unsafe.Pointer(&usernameBuffer[0])),
		uintptr(len(usernameBuffer)),
		uintptr(unsafe.Pointer(&passwordBuffer[0])),
		uintptr(len(passwordBuffer)),
		uintptr(unsafe.Pointer(&save)),
		flags,
	)
	if result == errorCancelled {
		return "", errors.New("authentication cancelled")
	}
	if result != 0 {
		if callErr != syscall.Errno(0) {
			return "", fmt.Errorf("Windows credential prompt failed (%d): %w", result, callErr)
		}
		return "", fmt.Errorf("Windows credential prompt failed with error %d", result)
	}
	return syscall.UTF16ToString(passwordBuffer[:]), nil
}
