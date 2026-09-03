package runtime

import (
	"io"
	"math"
	"math/big"

	"deedles.dev/writ/syntax"
)

// EncodeForm writes a source form in the tagged binary format.
// Quote, unquote, splice, and comment tags are valid here.
func EncodeForm(v syntax.Form) ([]byte, error) {
	var e enc
	if err := e.form(v); err != nil {
		return nil, err
	}
	return e.buf, nil
}

// DecodeForm reads one form from b.
func DecodeForm(b []byte) (syntax.Form, error) {
	return DecodeFormReader(newByteReader(b))
}

// DecodeFormReader reads one form from r.
func DecodeFormReader(r io.Reader) (syntax.Form, error) {
	d := dec{r: r}
	return d.form()
}

func (e *enc) form(v syntax.Form) error {
	switch v.Kind() {
	case syntax.KindInt:
		e.u8(tagInt)
		n := v.BigInt()
		if n != nil && !n.IsInt64() {
			e.u8(intCompactBig)
			e.blob(n.Bytes())
			sign := int8(1)
			if n.Sign() < 0 {
				sign = -1
			}
			e.i8(sign)
			return nil
		}
		e.u8(intCompactI64)
		if n != nil {
			e.i64(n.Int64())
			return nil
		}
		e.i64(0)
		return nil
	case syntax.KindFloat:
		e.u8(tagFloat)
		e.u64(math.Float64bits(v.Float64()))
		return nil
	case syntax.KindString:
		e.u8(tagString)
		e.str(v.Text())
		return nil
	case syntax.KindSymbol:
		e.u8(tagSymbol)
		e.str(v.Name())
		return nil
	case syntax.KindList:
		e.u8(tagList)
		if v.IsVec() {
			e.u8(1)
		} else {
			e.u8(0)
		}
		xs := v.Items()
		e.u32(uint32(len(xs)))
		for _, x := range xs {
			if err := e.form(x); err != nil {
				return err
			}
		}
		return nil
	case syntax.KindMap:
		e.u8(tagMap)
		pairs := v.Pairs()
		e.u32(uint32(len(pairs)))
		for _, p := range pairs {
			if err := e.form(p.Key); err != nil {
				return err
			}
			if err := e.form(p.Value); err != nil {
				return err
			}
		}
		return nil
	case syntax.KindQuote:
		e.u8(tagQuote)
		return e.form(v.Inner())
	case syntax.KindUnquote:
		e.u8(tagUnquote)
		return e.form(v.Inner())
	case syntax.KindSplice:
		e.u8(tagSplice)
		return e.form(v.Inner())
	case syntax.KindComment:
		e.u8(tagComment)
		e.str(v.CommentText())
		return nil
	default:
		return errf("cannot encode form %s", v.Kind())
	}
}

func (d *dec) form() (syntax.Form, error) {
	tag, err := d.u8()
	if err != nil {
		return syntax.Form{}, err
	}
	switch tag {
	case tagInt:
		mode, err := d.u8()
		if err != nil {
			return syntax.Form{}, err
		}
		switch mode {
		case intCompactI64:
			n, err := d.i64()
			if err != nil {
				return syntax.Form{}, err
			}
			return syntax.Int64(n), nil
		case intCompactBig:
			abs, err := d.blob()
			if err != nil {
				return syntax.Form{}, err
			}
			sign, err := d.i8()
			if err != nil {
				return syntax.Form{}, err
			}
			if sign != 1 && sign != -1 {
				return syntax.Form{}, errMsg("invalid int sign")
			}
			n := new(big.Int).SetBytes(abs)
			if sign < 0 {
				n.Neg(n)
			}
			return syntax.Int(n), nil
		default:
			return syntax.Form{}, errf("unknown int compact %d", mode)
		}
	case tagFloat:
		bits, err := d.u64()
		if err != nil {
			return syntax.Form{}, err
		}
		return syntax.Float(math.Float64frombits(bits)), nil
	case tagString:
		s, err := d.str()
		if err != nil {
			return syntax.Form{}, err
		}
		return syntax.String(s), nil
	case tagSymbol:
		s, err := d.str()
		if err != nil {
			return syntax.Form{}, err
		}
		return syntax.Symbol(s), nil
	case tagList:
		vec, err := d.u8()
		if err != nil {
			return syntax.Form{}, err
		}
		if vec != 0 && vec != 1 {
			return syntax.Form{}, errf("invalid list vecflag %d", vec)
		}
		n, err := d.u32()
		if err != nil {
			return syntax.Form{}, err
		}
		xs := make([]syntax.Form, n)
		for i := range xs {
			xs[i], err = d.form()
			if err != nil {
				return syntax.Form{}, err
			}
		}
		if vec == 1 {
			return syntax.List(xs...), nil
		}
		return syntax.CallList(xs...), nil
	case tagMap:
		n, err := d.u32()
		if err != nil {
			return syntax.Form{}, err
		}
		pairs := make([]syntax.MapPair, n)
		for i := range pairs {
			k, err := d.form()
			if err != nil {
				return syntax.Form{}, err
			}
			val, err := d.form()
			if err != nil {
				return syntax.Form{}, err
			}
			pairs[i] = syntax.MapPair{Key: k, Value: val}
		}
		return syntax.MapFrom(pairs...), nil
	case tagQuote:
		inner, err := d.form()
		if err != nil {
			return syntax.Form{}, err
		}
		return syntax.Quote(inner), nil
	case tagUnquote:
		inner, err := d.form()
		if err != nil {
			return syntax.Form{}, err
		}
		return syntax.Unquote(inner), nil
	case tagSplice:
		inner, err := d.form()
		if err != nil {
			return syntax.Form{}, err
		}
		return syntax.Splice(inner), nil
	case tagComment:
		s, err := d.str()
		if err != nil {
			return syntax.Form{}, err
		}
		return syntax.Comment(s), nil
	case tagHandle:
		return syntax.Form{}, errMsg("handles are not forms")
	default:
		return syntax.Form{}, errf("unknown form tag %d", tag)
	}
}
