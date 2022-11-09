package event

import "errors"

var ErrResponseNeedCall = errors.New(`Response need in call event`)
var ErrEventNotExists = errors.New(`event not exists`)
