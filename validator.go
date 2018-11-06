package assertly

import (
	"fmt"
	"github.com/viant/toolbox"
	"log"
	"path"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

const (
	MissingEntryViolation         = "entry was missing"
	IncompatibleDataTypeViolation = "data type was incompatible"
	KeyExistsViolation            = "key should exist"
	KeyDoesNotExistViolation      = "key should not exist"
	EqualViolation                = "value should be equal"
	NotEqualViolation             = "value should not be equal"
	LengthViolation               = "should have the same length"
	MissingCaseViolation          = "missing switch/case value"
	RegExprMatchesViolation       = "should match regrexp"
	RegExprDoesNotMatchViolation  = "should mot match regrexp"
	RangeViolation                = "should be in range"
	RangeNotViolation             = "should not be in range"
	ContainsViolation             = "should contain fragment"
	DoesNotContainViolation       = "should not contain fragment"
	PredicateViolation            = "should pass predicate"
	ValueWasNil                   = "should have not nil"
	SharedSwitchCaseKey           = "shared"
)

//Assert validates expected against actual data structure for supplied path
func Assert(expected, actual interface{}, path DataPath) (*Validation, error) {
	context := NewDefaultContext()
	return AssertWithContext(expected, actual, path, context)
}

func handleFailure(t *testing.T, args ...interface{}) {
	file, method, line := toolbox.DiscoverCaller(2, 10, "assert.go", "stack_helper.go", "validator.go")
	_, file = path.Split(file)
	fmt.Printf("%v:%v (%v)\n%v\n", file, line, method, fmt.Sprint(args))
	t.Fail()
}

//AssertWithContext validates expected against actual data structure for supplied path and context
func AssertWithContext(expected, actual interface{}, path DataPath, context *Context) (*Validation, error) {
	validation := NewValidation()
	err := assertValue(expected, actual, path, context, validation)
	return validation, err
}

func getPredicate(input interface{}) toolbox.Predicate {
	predicate, ok := input.(toolbox.Predicate)
	if !ok {
		if predicatePointer, ok := input.(*toolbox.Predicate); ok {
			predicate = *predicatePointer
		}
	}
	return predicate
}

func expandExpectedText(text string, path DataPath, context *Context) (interface{}, error) {
	if toolbox.IsCompleteJSON(text) {
		return asDataStructure(text), nil
	}
	if context.Evaluator.HasMacro(text) {
		evaluated, err := context.Evaluator.Expand(context.Context, text)
		if err != nil {
			return nil, fmt.Errorf("failed to expand macro %v, path:%v, %v", text, path.Path(), err)
		}
		if !toolbox.IsString(evaluated) {
			return evaluated, nil
		}
		text = toolbox.AsString(evaluated)
	}
	return text, nil
}

func assertTime(expected *time.Time, actual interface{}, path DataPath, context *Context, validation *Validation) (err error) {
	dateLayout := path.Match(context).DefaultTimeLayout()
	expectedTime, _ := toolbox.ToTime(expected, dateLayout)
	if actualTime, err := toolbox.ToTime(actual, dateLayout); err == nil && expectedTime != nil {
		actual = actualTime
		if expectedTime.Equal(*actualTime) {
			validation.PassedCount++
			return nil
		}
	}
	validation.AddFailure(NewFailure(path.Source(), path.Path(), EqualViolation, expected, actual))
	return nil
}

func assertValue(expected, actual interface{}, path DataPath, context *Context, validation *Validation) (err error) {
	if expected == nil {
		if actual == nil {
			validation.PassedCount++
			return nil
		}
		validation.AddFailure(NewFailure(path.Source(), path.Path(), NotEqualViolation, expected, actual))
		return
	}

	switch val := expected.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		assertInt(expected, actual, path, context, validation)
		return
	case float32, float64:
		assertFloat(expected, actual, path, context, validation)
		return
	case string:
		if expected, err = expandExpectedText(val, path, context); err != nil {
			return err
		}
	}

	switch val := actual.(type) {
	case string:
		if toolbox.IsCompleteJSON(val) {
			actual = asDataStructure(val)
		}
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		assertInt(expected, actual, path, context, validation)
		return
	case float32, float64:
		assertFloat(expected, actual, path, context, validation)
		return
	}

	predicate := getPredicate(expected)
	if predicate != nil {
		if !predicate.Apply(actual) {
			validation.AddFailure(NewFailure(path.Source(), path.Path(), PredicateViolation, fmt.Sprintf("%T%v", predicate, predicate), actual))
		} else {
			validation.PassedCount++
		}
		return nil
	}

	dateLayout := path.Match(context).DefaultTimeLayout()
	if toolbox.IsTime(expected) || toolbox.IsTime(actual) {
		expectedTime, _ := toolbox.ToTime(expected, dateLayout)
		return assertTime(expectedTime, actual, path, context, validation)

	} else if toolbox.IsStruct(expected) || (actual != nil && toolbox.IsStruct(actual)) {
		var converter = toolbox.NewColumnConverter(dateLayout)
		if toolbox.IsStruct(expected) {
			var expectedMap = make(map[string]interface{})
			converter.AssignConverted(&expectedMap, expected)
			expected = expectedMap
		}
		if toolbox.IsStruct(actual) {
			var actualMap = make(map[string]interface{})
			converter.AssignConverted(&actualMap, actual)
			actual = actualMap
		}
	}
	if toolbox.IsMap(expected) {
		return assertMap(toolbox.AsMap(expected), actual, path, context, validation)
	} else if toolbox.IsSlice(expected) {
		return assertSlice(toolbox.AsSlice(expected), actual, path, context, validation)
	} else if expected == actual || reflect.DeepEqual(expected, actual) {
		validation.PassedCount++
		return nil
	}
	directive := NewDirective(path)
	expectedText := toolbox.AsString(expected)

	if !context.StrictDatTypeCheck {
		expectedTime, err := toolbox.ToTime(expectedText, directive.DefaultTimeLayout())
		actualTime, e := toolbox.ToTime(actual, directive.DefaultTimeLayout())
		if e == nil || err == nil {
			if expectedTime == nil {
				if strings.HasPrefix(actualTime.String(), expectedText) {
					validation.PassedCount++
					return nil
				}
			} else if actualTime != nil {
				if actualTime.Equal(*expectedTime) {
					validation.PassedCount++
					return nil
				}
			}
		}
	}

	return assertText(toolbox.AsString(expected), toolbox.AsString(actual), path, context, validation)
}

func isNegated(candidate string) (string, bool) {
	isNot := strings.HasPrefix(candidate, "!")
	if isNot {
		candidate = string(candidate[1:])
	}
	return candidate, isNot
}

func assertRegExpr(isNegated bool, expected, actual string, path DataPath, context *Context, validation *Validation) error {
	expected = string(expected[2 : len(expected)-1])
	useMultiLine := strings.Count(actual, "\n") > 0
	pattern := ""
	if useMultiLine {
		pattern = "(?m)"
	}
	pattern += expected
	compiled, err := regexp.Compile(pattern)

	if err != nil {
		return fmt.Errorf("failed to compile %v, path: %v, %v", expected, path, err)
	}
	var matches = compiled.Match(([]byte)(actual))
	if !matches && !isNegated {
		validation.AddFailure(NewFailure(path.Source(), path.Path(), RegExprMatchesViolation, expected, actual))
	} else if matches && isNegated {
		validation.AddFailure(NewFailure(path.Source(), path.Path(), RegExprDoesNotMatchViolation, expected, actual))
	} else {
		validation.PassedCount++
	}
	return nil
}

func assertRange(isNegated bool, expected, actual string, path DataPath, context *Context, validation *Validation) error {
	if strings.Count(expected, "..")+strings.Count(expected, ",") == 0 {
		return fmt.Errorf("invalid range format, expected /[min..max]/ or /[val1,val2,valN]/, but had:%v, path: %v", expected, path.Path())
	}
	actual = strings.TrimSpace(actual)
	expected = string(expected[2 : len(expected)-2])
	var rangeValues = strings.Split(expected, "..")

	var withinRange bool
	if len(rangeValues) > 1 {
		var minExpected = toolbox.AsFloat(strings.TrimSpace(rangeValues[0]))
		var maxExpected = toolbox.AsFloat(strings.TrimSpace(rangeValues[1]))
		var actualNumber = toolbox.AsFloat(actual)
		withinRange = actualNumber >= minExpected && actualNumber <= maxExpected
	} else {
		rangeValues = strings.Split(expected, ",")
		for _, candidate := range rangeValues {
			if strings.TrimSpace(candidate) == actual {
				withinRange = true
				break
			}
		}
	}
	if !withinRange && !isNegated {
		validation.AddFailure(NewFailure(path.Source(), path.Path(), RangeViolation, expected, actual))
	} else if withinRange && isNegated {
		validation.AddFailure(NewFailure(path.Source(), path.Path(), RangeNotViolation, expected, actual))
	} else {
		validation.PassedCount++
	}
	return nil
}

func assertContains(isNegated bool, expected, actual string, path DataPath, context *Context, validation *Validation) {
	expected = string(expected[1 : len(expected)-1])
	contains := strings.Contains(actual, expected)

	if !contains && !isNegated {
		validation.AddFailure(NewFailure(path.Source(), path.Path(), ContainsViolation, expected, actual))
	} else if contains && isNegated {
		validation.AddFailure(NewFailure(path.Source(), path.Path(), DoesNotContainViolation, expected, actual))
	} else {
		validation.PassedCount++
	}
}

func assertText(expected, actual string, path DataPath, context *Context, validation *Validation) error {
	expected = strings.TrimSpace(expected)
	if strings.HasSuffix(expected, "/") {
		expected, isNegated := isNegated(expected)
		isRegExpr := strings.HasPrefix(expected, "~/")
		if isRegExpr {
			return assertRegExpr(isNegated, expected, actual, path, context, validation)
		}
		isRangeExpr := (strings.HasPrefix(expected, "/[") || strings.HasPrefix(expected, "!/[")) && strings.HasSuffix(expected, "]/")
		if isRangeExpr {
			return assertRange(isNegated, expected, actual, path, context, validation)
		}
		isContains := strings.HasPrefix(expected, "/")
		if isContains {
			assertContains(isNegated, expected, actual, path, context, validation)
			return nil
		}
	}
	expected, isNegated := isNegated(expected)
	isEqual := expected == actual
	if !isEqual && !isNegated {
		validation.AddFailure(NewFailure(path.Source(), path.Path(), EqualViolation, expected, actual))
	} else if isEqual && isNegated {
		validation.AddFailure(NewFailure(path.Source(), path.Path(), NotEqualViolation, expected, actual))
	} else {
		validation.PassedCount++
	}
	return nil
}

func actualMap(expected, actualValue interface{}, path DataPath, directive *Directive, validation *Validation) map[string]interface{} {
	var actual map[string]interface{}
	if toolbox.IsMap(actualValue) {
		actual = toolbox.AsMap(actualValue)
	} else if toolbox.IsSlice(actualValue) {
		if len(directive.IndexBy) == 0 {
			validation.AddFailure(NewFailure(path.Source(), path.Path(), IncompatibleDataTypeViolation, expected, actualValue))
			return nil
		}
		aSlice := toolbox.AsSlice(actualValue)
		actual = indexSliceBy(aSlice, directive.IndexBy...)
	} else {
		validation.AddFailure(NewFailure(path.Source(), path.Path(), IncompatibleDataTypeViolation, expected, actualValue))
		return nil
	}
	return actual
}

func assertInt(expected, actual interface{}, path DataPath, context *Context, validation *Validation) {
	expectedInt, err := toolbox.ToInt(expected)
	if err != nil {
		assertText(toolbox.AsString(expected), toolbox.AsString(actual), path, context, validation)
		return
	}
	actualInt, err := toolbox.ToInt(actual)
	isEqual := err == nil && expectedInt == actualInt
	if !isEqual {
		if text, ok := expected.(string); ok {
			if strings.HasPrefix(text, "/") || strings.HasPrefix(text, "!") {
				assertText(toolbox.AsString(expected), toolbox.AsString(actual), path, context, validation)
				return
			}
		}
		validation.AddFailure(NewFailure(path.Source(), path.Path(), EqualViolation, expected, actual))
	} else {
		validation.PassedCount++
	}
}

func assertFloat(expected, actual interface{}, path DataPath, context *Context, validation *Validation) {
	expectedFloat, err := toolbox.ToFloat(expected)
	isEqual := err == nil && expectedFloat == toolbox.AsFloat(actual)
	if !isEqual {

		if text, ok := expected.(string); ok {
			if strings.HasPrefix(text, "/") || strings.HasPrefix(text, "!") {
				assertText(toolbox.AsString(expected), toolbox.AsString(actual), path, context, validation)
				return
			}
		}
		validation.AddFailure(NewFailure(path.Source(), path.Path(), EqualViolation, expected, actual))
	} else {
		validation.PassedCount++
	}
}

func assertMap(expected map[string]interface{}, actualValue interface{}, path DataPath, context *Context, validation *Validation) error {
	if actualValue == nil {
		if expected == nil {
			validation.PassedCount++
			return nil
		}
		validation.AddFailure(NewFailure(path.Source(), path.Path(), ValueWasNil, nil, expected))
		return nil
	}
	directive := NewDirective(path)
	directive.mergeFrom(path.Match(context))
	directive.ExtractDirectives(expected)
	path.SetSource(directive.Source)
	var actual = actualMap(expected, actualValue, path, directive, validation)
	if actual == nil {
		return nil
	}
	directive.ExtractDataTypes(actual)
	if err := directive.Apply(actual); err != nil {
		log.Print("failed to apply directive to actual actual value: " + err.Error())
	}

	if len(directive.SwitchBy) > 0 {
		switchValue := keysValue(actual, directive.SwitchBy...)
		caseValue, ok := expected[switchValue]
		if !ok {
			validation.AddFailure(NewFailure(path.Source(), path.Path(), MissingCaseViolation, expected, actual, directive.SwitchBy, switchValue))
			return nil
		}
		if !toolbox.IsMap(caseValue) {
			return fmt.Errorf("case value should be map but was %T, path: %v", caseValue, path.Path())
		}

		caseValueMap := toolbox.AsMap(caseValue)
		if shared, ok := expected[SharedSwitchCaseKey]; ok && toolbox.IsMap(shared) {
			for k, v := range toolbox.AsMap(shared) {
				caseValueMap[k] = v
			}
		}
		expected = caseValueMap
	}

	if err := directive.Apply(expected); err != nil {
		log.Print("failed to apply driected to expected value:" + err.Error())
	}

	indexable := isIndexable(expected)
	if len(directive.IndexBy) == 0 {
		indexable = false
	}
	for expectedKey, expectedValue := range expected {
		val := expected[expectedKey]
		if val == nil || toolbox.AsString(val) == "" {
			continue
		}
		if directive.IsDirectiveKey(expectedKey) {
			continue
		}
		var keyPath DataPath
		if indexable && toolbox.IsMap(expectedValue) {
			keyPath = path.Key(keysPairValue(toolbox.AsMap(expectedValue), directive.IndexBy...))
		} else {
			keyPath = path.Key(expectedKey)
		}
		actualValue, ok := actual[expectedKey]
		if directive.KeyDoesNotExist[expectedKey] {
			if ok {
				validation.AddFailure(NewFailure(keyPath.Source(), keyPath.Path(), KeyDoesNotExistViolation, expectedKey, expectedKey))
			} else {
				validation.PassedCount++
			}
			continue
		}

		if directive.KeyExists[expectedKey] {
			if !ok {
				availableKeys := toolbox.MapKeysToStringSlice(expected)
				validation.AddFailure(NewFailure(keyPath.Source(), keyPath.Path(), KeyExistsViolation, expectedKey, strings.Join(availableKeys, ",")))
			} else {
				validation.PassedCount++
			}
			continue
		}

		if !ok {
			key := "key:" + expectedKey
			validation.AddFailure(NewFailure(keyPath.Source(), keyPath.Path(), MissingEntryViolation, expectedValue, toolbox.MapKeysToStringSlice(actual), key))
			continue
		}
		if err := assertValue(expectedValue, actualValue, keyPath, context, validation); err != nil {
			return err
		}
	}
	return nil
}

func asCaseInsensitiveSlice(aSlice []interface{}) []interface{} {
	var result = make([]interface{}, 0)
	for _, item := range aSlice {
		result = append(result, asCaseInsensitiveMap(toolbox.AsMap(item)))
	}
	return result
}

func asCaseInsensitiveMap(aMap map[string]interface{}) map[string]interface{} {
	var result = make(map[string]interface{})
	for k, v := range aMap {
		result[strings.ToUpper(k)] = v
	}
	return result
}

func assertSlice(expected []interface{}, actualValue interface{}, path DataPath, context *Context, validation *Validation) error {
	if actualValue == nil {
		validation.AddFailure(NewFailure(path.Source(), path.Path(), IncompatibleDataTypeViolation, expected, actualValue))
		return nil
	}
	if !toolbox.IsSlice(actualValue) {
		validation.AddFailure(NewFailure(path.Source(), path.Path(), IncompatibleDataTypeViolation, expected, actualValue))
		return nil
	}
	var actual = toolbox.AsSlice(actualValue)
	if len(expected) == 0 {
		if len(expected) == len(actual) {
			validation.PassedCount++
			return nil
		}
		validation.AddFailure(NewFailure(path.Source(), path.Path(), LengthViolation, len(expected), len(actual)))
		return nil
	}
	directive := path.Match(context)

	if toolbox.IsMap(expected[0]) || toolbox.IsStruct(expected[0]) {
		first := toolbox.AsMap(expected[0])
		if directive.ExtractDirectives(first) {
			expected = expected[1:]
		}

		if directive.SortText {
			var expectedSlice = []string{}
			toolbox.ProcessSlice(expected, func(item interface{}) bool {
				expectedSlice = append(expectedSlice, toolbox.AsString(item))
				return true
			})
			var actualSlice = []string{}
			toolbox.ProcessSlice(expected, func(item interface{}) bool {
				actualSlice = append(actualSlice, toolbox.AsString(item))
				return true
			})

			sort.Strings(expectedSlice)
			expected = []interface{}{}
			for _, item := range expectedSlice {
				expected = append(expected, item)
			}

			sort.Strings(actualSlice)
			actual = []interface{}{}
			for _, item := range actualSlice {
				actual = append(actual, item)
			}

		} else {

			if !directive.CaseSensitive {
				expected = asCaseInsensitiveSlice(expected)
				actual = asCaseInsensitiveSlice(actual)
				directive.ApplyCaseInsensitive()
			}

			for i := 0; i < len(actual); i++ {
				var actualMap = toolbox.AsMap(actual[i])
				directive.ExtractDataTypes(actualMap)
			}

			//add directive to expected
			for i := 0; i < len(expected); i++ {
				var expectedMap = toolbox.AsMap(expected[i])
				directive.Add(expectedMap)
				directive.Apply(expectedMap)
				expected[i] = expectedMap
				if i < len(actual) {
					actualMap := toolbox.AsMap(actual[i])
					directive.Apply(actualMap)
					actual[i] = actualMap
				}
			}

			shouldIndex := len(directive.IndexBy) > 0
			if shouldIndex {
				expectedMap := indexSliceBy(expected, directive.IndexBy...)
				actualMap := indexSliceBy(actual, directive.IndexBy...)
				return assertMap(expectedMap, actualMap, path, context, validation)
			}
		}
	}

	for i := 0; i < len(expected); i++ {
		if i >= len(actual) {
			validation.AddFailure(NewFailure(path.Source(), path.Path(), LengthViolation, len(expected), len(actual)))
			return nil
		}
		indexPath := path.Index(i)
		if err := assertValue(expected[i], actual[i], indexPath, context, validation); err != nil {
			return err
		}
	}
	return nil
}
