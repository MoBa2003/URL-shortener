package shortener

import "errors"

var NotFoundErr = errors.New("code not found")
var InvalidURLErr = errors.New("invalid url")
var CodeGenerationFailed = errors.New("Code Generation Failed")
