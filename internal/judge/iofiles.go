package judge

import (
	"errors"
	"strings"
)

// 文件 IO 的两个文件名最终会变成工作目录里的路径，所以它们只能是一个平坦的
// 文件名：没有目录分隔符、没有 ..、不是 . 或 ..，也不会撞上判机自己的文件
// （源码、护栏、可执行程序）。长度给到 64，够 test.in 这类老题面用。
//
// 出题表单与判机共用这一条规则：表单拦一次是给出题人看，判机再拦一次是因为
// 老数据、直接改库或不认识这条规则的新调用方都可能绕过表单。
const maxIOFileName = 64

var reservedIONames = map[string]bool{
	"main.cpp": true, "main.c": true, "main.py": true,
	"pyguard.py": true, "program": true, "program.exe": true,
	// Windows 保留设备名：判机也可能在 Windows 上跑，落一个 con 进去会静默变成设备。
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// ValidIOFileName 报告一个文件名能不能安全地落进提交自己的工作目录。
func ValidIOFileName(name string) bool {
	if name == "" || len(name) > maxIOFileName {
		return false
	}
	if name == "." || name == ".." || strings.HasPrefix(name, ".") {
		return false
	}
	if strings.HasSuffix(name, ".") {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '.', c == '_', c == '-':
		default:
			return false
		}
	}
	return !reservedIONames[strings.ToLower(name)]
}

// ValidateIOFiles 检查成对语义：两个都空是 stdin/stdout 题；只填一个说明出题
// 人漏了另一半；两个都填时它们必须不同。名字本身还要过 ValidIOFileName。
func ValidateIOFiles(in, out string) error {
	if in == "" && out == "" {
		return nil
	}
	if in == "" || out == "" {
		return errors.New("文件 IO 的输入与输出文件名要成对填写，或者都留空走标准输入输出")
	}
	if !ValidIOFileName(in) {
		return errors.New("输入文件名不合法：只允许字母、数字、点、下划线与连字符，且不能占用判机文件名")
	}
	if !ValidIOFileName(out) {
		return errors.New("输出文件名不合法：只允许字母、数字、点、下划线与连字符，且不能占用判机文件名")
	}
	if strings.EqualFold(in, out) {
		return errors.New("输入与输出文件名不能相同")
	}
	return nil
}
