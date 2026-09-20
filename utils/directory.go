package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"InkFlow/global"

	"go.uber.org/zap"
)

// FileSHA256 calculates the SHA-256 digest of a file stream without loading the
// whole file into memory. The reader is consumed until EOF.
func FileSHA256(reader io.Reader) (string, error) {
	if reader == nil {
		return "", errors.New("file reader is nil")
	}
	hash := sha256.New()
	buf := make([]byte, 32*1024)
	if _, err := io.CopyBuffer(hash, reader, buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

//@author: [piexlmax](https://github.com/piexlmax)
//@function: PathExists
//@description: 文件目录是否存在
//@param: path string
//@return: bool, error

func PathExists(path string) (bool, error) {
	fi, err := os.Stat(path)
	if err == nil {
		if fi.IsDir() {
			return true, nil
		}
		return false, errors.New("存在同名文件")
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

//@author: [piexlmax](https://github.com/piexlmax)
//@function: CreateDir
//@description: 批量创建文件夹
//@param: dirs ...string
//@return: err error

func CreateDir(dirs ...string) (err error) {
	for _, v := range dirs {
		exist, err := PathExists(v)
		if err != nil {
			return err
		}
		if !exist {
			global.GVA_LOG.Debug("create directory" + v)
			if err := os.MkdirAll(v, os.ModePerm); err != nil {
				global.GVA_LOG.Error("create directory"+v, zap.Any(" error:", err))
				return err
			}
		}
	}
	return err
}

// 辅助函数：复制本地文件
func CopyLocalFile(src, dst string) error {
	sourceFileStat, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !sourceFileStat.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", src)
	}
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()
	_, err = io.Copy(destination, source)
	return err
}

// 辅助函数：过滤非法文件名字符
func FilterFileName(name string) string {
	// 定义需要替换的非法字符列表 (Windows/Linux 文件系统保留字符)
	illegalChars := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"}

	// 遍历并替换
	for _, char := range illegalChars {
		name = strings.ReplaceAll(name, char, "_")
	}

	// 可选：你也可以顺便把换行符去掉，防止文件名换行
	name = strings.ReplaceAll(name, "\n", "")
	name = strings.ReplaceAll(name, "\r", "")

	return name
}
