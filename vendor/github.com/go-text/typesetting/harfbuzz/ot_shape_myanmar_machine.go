package harfbuzz

// Code generated with ragel -Z -o ot_myanmar_machine.go ot_myanmar_machine.rl ; sed -i '/^\/\/line/ d' ot_myanmar_machine.go ; goimports -w ot_myanmar_machine.go  DO NOT EDIT.

// ported from harfbuzz/src/hb-ot-shape-complex-myanmar-machine.rl Copyright © 2015 Mozilla Foundation. Google, Inc. Behdad Esfahbod

// myanmar_syllable_type_t
const (
	myanmarConsonantSyllable = iota
	myanmarBrokenCluster
	myanmarNonMyanmarCluster
)

const myaSM_ex_A = 9
const myaSM_ex_As = 32
const myaSM_ex_C = 1
const myaSM_ex_CS = 18
const myaSM_ex_DB = 3
const myaSM_ex_DOTTEDCIRCLE = 11
const myaSM_ex_GB = 10
const myaSM_ex_H = 4
const myaSM_ex_IV = 2
const myaSM_ex_MH = 35
const myaSM_ex_ML = 41
const myaSM_ex_MR = 36
const myaSM_ex_MW = 37
const myaSM_ex_MY = 38
const myaSM_ex_PT = 39
const myaSM_ex_Ra = 15
const myaSM_ex_SM = 8
const myaSM_ex_SMPst = 57
const myaSM_ex_VAbv = 20
const myaSM_ex_VBlw = 21
const myaSM_ex_VPre = 22
const myaSM_ex_VPst = 23
const myaSM_ex_VS = 40
const myaSM_ex_ZWJ = 6
const myaSM_ex_ZWNJ = 5

var _myaSM_actions []byte = []byte{
	0, 1, 0, 1, 1, 1, 5, 1, 6,
	1, 7, 1, 8, 1, 9, 1, 10,
	1, 11, 1, 12, 2, 2, 3, 2,
	2, 4,
}

var _myaSM_key_offsets []int16 = []int16{
	0, 25, 44, 51, 56, 64, 70, 82,
	90, 99, 109, 120, 126, 129, 139, 148,
	160, 171, 188, 201, 213, 227, 241, 257,
	272, 290, 297, 302, 310, 316, 328, 336,
	345, 355, 366, 372, 375, 394, 404, 413,
	425, 436, 453, 466, 478, 492, 506, 522,
	537, 555, 574, 592, 616,
}

var _myaSM_trans_keys []byte = []byte{
	3, 4, 8, 9, 15, 18, 20, 21,
	22, 23, 32, 35, 36, 37, 38, 39,
	40, 41, 57, 1, 2, 5, 6, 10,
	11, 3, 4, 8, 9, 20, 21, 22,
	23, 32, 35, 36, 37, 38, 39, 40,
	41, 57, 5, 6, 8, 23, 32, 39,
	57, 5, 6, 8, 39, 57, 5, 6,
	3, 8, 9, 32, 39, 57, 5, 6,
	8, 32, 39, 57, 5, 6, 3, 8,
	9, 20, 23, 32, 35, 39, 41, 57,
	5, 6, 3, 8, 9, 23, 39, 57,
	5, 6, 3, 8, 9, 20, 23, 39,
	57, 5, 6, 3, 8, 9, 20, 23,
	32, 39, 57, 5, 6, 3, 8, 9,
	20, 23, 32, 39, 41, 57, 5, 6,
	8, 23, 39, 57, 5, 6, 15, 1,
	2, 3, 8, 9, 20, 21, 23, 39,
	57, 5, 6, 3, 8, 9, 21, 23,
	39, 57, 5, 6, 3, 8, 9, 20,
	21, 22, 23, 39, 40, 57, 5, 6,
	3, 8, 9, 20, 21, 22, 23, 39,
	57, 5, 6, 3, 8, 9, 20, 21,
	22, 23, 32, 35, 36, 37, 38, 39,
	41, 57, 5, 6, 3, 8, 9, 20,
	21, 22, 23, 32, 39, 41, 57, 5,
	6, 3, 8, 9, 20, 21, 22, 23,
	32, 39, 57, 5, 6, 3, 8, 9,
	20, 21, 22, 23, 35, 37, 39, 41,
	57, 5, 6, 3, 8, 9, 20, 21,
	22, 23, 32, 35, 39, 41, 57, 5,
	6, 3, 8, 9, 20, 21, 22, 23,
	32, 35, 36, 37, 39, 41, 57, 5,
	6, 3, 8, 9, 20, 21, 22, 23,
	35, 36, 37, 39, 41, 57, 5, 6,
	3, 4, 8, 9, 20, 21, 22, 23,
	32, 35, 36, 37, 38, 39, 41, 57,
	5, 6, 8, 23, 32, 39, 57, 5,
	6, 8, 39, 57, 5, 6, 3, 8,
	9, 32, 39, 57, 5, 6, 8, 32,
	39, 57, 5, 6, 3, 8, 9, 20,
	23, 32, 35, 39, 41, 57, 5, 6,
	3, 8, 9, 23, 39, 57, 5, 6,
	3, 8, 9, 20, 23, 39, 57, 5,
	6, 3, 8, 9, 20, 23, 32, 39,
	57, 5, 6, 3, 8, 9, 20, 23,
	32, 39, 41, 57, 5, 6, 8, 23,
	39, 57, 5, 6, 15, 1, 2, 3,
	4, 8, 9, 20, 21, 22, 23, 32,
	35, 36, 37, 38, 39, 40, 41, 57,
	5, 6, 3, 8, 9, 20, 21, 23,
	39, 57, 5, 6, 3, 8, 9, 21,
	23, 39, 57, 5, 6, 3, 8, 9,
	20, 21, 22, 23, 39, 40, 57, 5,
	6, 3, 8, 9, 20, 21, 22, 23,
	39, 57, 5, 6, 3, 8, 9, 20,
	21, 22, 23, 32, 35, 36, 37, 38,
	39, 41, 57, 5, 6, 3, 8, 9,
	20, 21, 22, 23, 32, 39, 41, 57,
	5, 6, 3, 8, 9, 20, 21, 22,
	23, 32, 39, 57, 5, 6, 3, 8,
	9, 20, 21, 22, 23, 35, 37, 39,
	41, 57, 5, 6, 3, 8, 9, 20,
	21, 22, 23, 32, 35, 39, 41, 57,
	5, 6, 3, 8, 9, 20, 21, 22,
	23, 32, 35, 36, 37, 39, 41, 57,
	5, 6, 3, 8, 9, 20, 21, 22,
	23, 35, 36, 37, 39, 41, 57, 5,
	6, 3, 4, 8, 9, 20, 21, 22,
	23, 32, 35, 36, 37, 38, 39, 41,
	57, 5, 6, 3, 4, 8, 9, 20,
	21, 22, 23, 32, 35, 36, 37, 38,
	39, 40, 41, 57, 5, 6, 3, 4,
	8, 9, 20, 21, 22, 23, 32, 35,
	36, 37, 38, 39, 41, 57, 5, 6,
	3, 4, 8, 9, 15, 20, 21, 22,
	23, 32, 35, 36, 37, 38, 39, 40,
	41, 57, 1, 2, 5, 6, 10, 11,
	15, 1, 2, 10, 11,
}

var _myaSM_single_lengths []byte = []byte{
	19, 17, 5, 3, 6, 4, 10, 6,
	7, 8, 9, 4, 1, 8, 7, 10,
	9, 15, 11, 10, 12, 12, 14, 13,
	16, 5, 3, 6, 4, 10, 6, 7,
	8, 9, 4, 1, 17, 8, 7, 10,
	9, 15, 11, 10, 12, 12, 14, 13,
	16, 17, 16, 18, 1,
}

var _myaSM_range_lengths []byte = []byte{
	3, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 1, 1, 1, 1, 1,
	1, 1, 1, 3, 2,
}

var _myaSM_index_offsets []int16 = []int16{
	0, 23, 42, 49, 54, 62, 68, 80,
	88, 97, 107, 118, 124, 127, 137, 146,
	158, 169, 186, 199, 211, 225, 239, 255,
	270, 288, 295, 300, 308, 314, 326, 334,
	343, 353, 364, 370, 373, 392, 402, 411,
	423, 434, 451, 464, 476, 490, 504, 520,
	535, 553, 572, 590, 612,
}

var _myaSM_indicies []byte = []byte{
	2, 3, 5, 6, 7, 8, 9, 10,
	11, 12, 13, 14, 15, 16, 17, 18,
	19, 20, 21, 1, 4, 1, 0, 23,
	24, 26, 27, 28, 29, 30, 31, 32,
	33, 34, 35, 36, 37, 38, 39, 26,
	25, 22, 26, 31, 40, 37, 26, 25,
	22, 26, 37, 26, 25, 22, 41, 26,
	37, 26, 37, 26, 25, 22, 26, 26,
	37, 26, 25, 22, 23, 26, 27, 42,
	31, 43, 44, 37, 43, 26, 25, 22,
	23, 26, 27, 31, 37, 26, 25, 22,
	23, 26, 27, 42, 31, 37, 26, 25,
	22, 23, 26, 27, 42, 31, 43, 37,
	26, 25, 22, 23, 26, 27, 42, 31,
	43, 37, 43, 26, 25, 22, 26, 31,
	37, 26, 25, 22, 1, 1, 22, 23,
	26, 27, 28, 29, 31, 37, 26, 25,
	22, 23, 26, 27, 29, 31, 37, 26,
	25, 22, 23, 26, 27, 28, 29, 30,
	31, 37, 45, 26, 25, 22, 23, 26,
	27, 28, 29, 30, 31, 37, 26, 25,
	22, 23, 26, 27, 28, 29, 30, 31,
	32, 33, 34, 35, 36, 37, 39, 26,
	25, 22, 23, 26, 27, 28, 29, 30,
	31, 45, 37, 39, 26, 25, 22, 23,
	26, 27, 28, 29, 30, 31, 45, 37,
	26, 25, 22, 23, 26, 27, 28, 29,
	30, 31, 33, 35, 37, 39, 26, 25,
	22, 23, 26, 27, 28, 29, 30, 31,
	45, 33, 37, 39, 26, 25, 22, 23,
	26, 27, 28, 29, 30, 31, 46, 33,
	34, 35, 37, 39, 26, 25, 22, 23,
	26, 27, 28, 29, 30, 31, 33, 34,
	35, 37, 39, 26, 25, 22, 23, 24,
	26, 27, 28, 29, 30, 31, 32, 33,
	34, 35, 36, 37, 39, 26, 25, 22,
	5, 12, 49, 18, 5, 48, 47, 5,
	18, 5, 48, 50, 51, 5, 18, 5,
	18, 5, 48, 47, 5, 5, 18, 5,
	48, 47, 2, 5, 6, 52, 12, 53,
	54, 18, 53, 5, 48, 47, 2, 5,
	6, 12, 18, 5, 48, 47, 2, 5,
	6, 52, 12, 18, 5, 48, 47, 2,
	5, 6, 52, 12, 53, 18, 5, 48,
	47, 2, 5, 6, 52, 12, 53, 18,
	53, 5, 48, 47, 5, 12, 18, 5,
	48, 47, 55, 55, 47, 2, 3, 5,
	6, 9, 10, 11, 12, 13, 14, 15,
	16, 17, 18, 19, 20, 5, 48, 47,
	2, 5, 6, 9, 10, 12, 18, 5,
	48, 47, 2, 5, 6, 10, 12, 18,
	5, 48, 47, 2, 5, 6, 9, 10,
	11, 12, 18, 56, 5, 48, 47, 2,
	5, 6, 9, 10, 11, 12, 18, 5,
	48, 47, 2, 5, 6, 9, 10, 11,
	12, 13, 14, 15, 16, 17, 18, 20,
	5, 48, 47, 2, 5, 6, 9, 10,
	11, 12, 56, 18, 20, 5, 48, 47,
	2, 5, 6, 9, 10, 11, 12, 56,
	18, 5, 48, 47, 2, 5, 6, 9,
	10, 11, 12, 14, 16, 18, 20, 5,
	48, 47, 2, 5, 6, 9, 10, 11,
	12, 56, 14, 18, 20, 5, 48, 47,
	2, 5, 6, 9, 10, 11, 12, 57,
	14, 15, 16, 18, 20, 5, 48, 47,
	2, 5, 6, 9, 10, 11, 12, 14,
	15, 16, 18, 20, 5, 48, 47, 2,
	3, 5, 6, 9, 10, 11, 12, 13,
	14, 15, 16, 17, 18, 20, 5, 48,
	47, 23, 24, 26, 27, 28, 29, 30,
	31, 58, 33, 34, 35, 36, 37, 38,
	39, 26, 25, 22, 23, 59, 26, 27,
	28, 29, 30, 31, 32, 33, 34, 35,
	36, 37, 39, 26, 25, 22, 2, 3,
	5, 6, 1, 9, 10, 11, 12, 13,
	14, 15, 16, 17, 18, 19, 20, 5,
	1, 48, 1, 47, 1, 1, 1, 60,
}

var _myaSM_trans_targs []byte = []byte{
	0, 1, 25, 35, 0, 26, 30, 49,
	52, 37, 38, 39, 29, 41, 42, 44,
	45, 46, 27, 48, 43, 26, 0, 2,
	12, 0, 3, 7, 13, 14, 15, 6,
	17, 18, 20, 21, 22, 4, 24, 19,
	11, 5, 8, 9, 10, 16, 23, 0,
	0, 34, 0, 28, 31, 32, 33, 36,
	40, 47, 50, 51, 0,
}

var _myaSM_trans_actions []byte = []byte{
	11, 0, 0, 0, 7, 24, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 21, 13, 0,
	0, 5, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 15,
	9, 0, 19, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 17,
}

var _myaSM_to_state_actions []byte = []byte{
	1, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0,
}

var _myaSM_from_state_actions []byte = []byte{
	3, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0,
}

var _myaSM_eof_trans []int16 = []int16{
	0, 23, 23, 23, 23, 23, 23, 23,
	23, 23, 23, 23, 23, 23, 23, 23,
	23, 23, 23, 23, 23, 23, 23, 23,
	23, 48, 51, 48, 48, 48, 48, 48,
	48, 48, 48, 48, 48, 48, 48, 48,
	48, 48, 48, 48, 48, 48, 48, 48,
	48, 23, 23, 48, 61,
}

const myaSM_start int = 0
const myaSM_first_final int = 0
const myaSM_error int = -1

const myaSM_en_main int = 0

func findSyllablesMyanmar(buffer *Buffer) {
	var p, ts, te, act, cs int
	info := buffer.Info

	{
		cs = myaSM_start
		ts = 0
		te = 0
		act = 0
	}

	pe := len(info)
	eof := pe

	var syllableSerial uint8 = 1

	{
		var _klen int
		var _trans int
		var _acts int
		var _nacts uint
		var _keys int
		if p == pe {
			goto _test_eof
		}
	_resume:
		_acts = int(_myaSM_from_state_actions[cs])
		_nacts = uint(_myaSM_actions[_acts])
		_acts++
		for ; _nacts > 0; _nacts-- {
			_acts++
			switch _myaSM_actions[_acts-1] {
			case 1:
				ts = p

			}
		}

		_keys = int(_myaSM_key_offsets[cs])
		_trans = int(_myaSM_index_offsets[cs])

		_klen = int(_myaSM_single_lengths[cs])
		if _klen > 0 {
			_lower := int(_keys)
			var _mid int
			_upper := int(_keys + _klen - 1)
			for {
				if _upper < _lower {
					break
				}

				_mid = _lower + ((_upper - _lower) >> 1)
				switch {
				case (info[p].complexCategory) < _myaSM_trans_keys[_mid]:
					_upper = _mid - 1
				case (info[p].complexCategory) > _myaSM_trans_keys[_mid]:
					_lower = _mid + 1
				default:
					_trans += int(_mid - int(_keys))
					goto _match
				}
			}
			_keys += _klen
			_trans += _klen
		}

		_klen = int(_myaSM_range_lengths[cs])
		if _klen > 0 {
			_lower := int(_keys)
			var _mid int
			_upper := int(_keys + (_klen << 1) - 2)
			for {
				if _upper < _lower {
					break
				}

				_mid = _lower + (((_upper - _lower) >> 1) & ^1)
				switch {
				case (info[p].complexCategory) < _myaSM_trans_keys[_mid]:
					_upper = _mid - 2
				case (info[p].complexCategory) > _myaSM_trans_keys[_mid+1]:
					_lower = _mid + 2
				default:
					_trans += int((_mid - int(_keys)) >> 1)
					goto _match
				}
			}
			_trans += _klen
		}

	_match:
		_trans = int(_myaSM_indicies[_trans])
	_eof_trans:
		cs = int(_myaSM_trans_targs[_trans])

		if _myaSM_trans_actions[_trans] == 0 {
			goto _again
		}

		_acts = int(_myaSM_trans_actions[_trans])
		_nacts = uint(_myaSM_actions[_acts])
		_acts++
		for ; _nacts > 0; _nacts-- {
			_acts++
			switch _myaSM_actions[_acts-1] {
			case 2:
				te = p + 1

			case 3:
				act = 2
			case 4:
				act = 3
			case 5:
				te = p + 1
				{
					foundSyllableMyanmar(myanmarConsonantSyllable, ts, te, info, &syllableSerial)
				}
			case 6:
				te = p + 1
				{
					foundSyllableMyanmar(myanmarNonMyanmarCluster, ts, te, info, &syllableSerial)
				}
			case 7:
				te = p + 1
				{
					foundSyllableMyanmar(myanmarBrokenCluster, ts, te, info, &syllableSerial)
					buffer.scratchFlags |= bsfHasBrokenSyllable
				}
			case 8:
				te = p + 1
				{
					foundSyllableMyanmar(myanmarNonMyanmarCluster, ts, te, info, &syllableSerial)
				}
			case 9:
				te = p
				p--
				{
					foundSyllableMyanmar(myanmarConsonantSyllable, ts, te, info, &syllableSerial)
				}
			case 10:
				te = p
				p--
				{
					foundSyllableMyanmar(myanmarBrokenCluster, ts, te, info, &syllableSerial)
					buffer.scratchFlags |= bsfHasBrokenSyllable
				}
			case 11:
				te = p
				p--
				{
					foundSyllableMyanmar(myanmarNonMyanmarCluster, ts, te, info, &syllableSerial)
				}
			case 12:
				switch act {
				case 2:
					{
						p = (te) - 1
						foundSyllableMyanmar(myanmarNonMyanmarCluster, ts, te, info, &syllableSerial)
					}
				case 3:
					{
						p = (te) - 1
						foundSyllableMyanmar(myanmarBrokenCluster, ts, te, info, &syllableSerial)
						buffer.scratchFlags |= bsfHasBrokenSyllable
					}
				}

			}
		}

	_again:
		_acts = int(_myaSM_to_state_actions[cs])
		_nacts = uint(_myaSM_actions[_acts])
		_acts++
		for ; _nacts > 0; _nacts-- {
			_acts++
			switch _myaSM_actions[_acts-1] {
			case 0:
				ts = 0

			}
		}

		p++
		if p != pe {
			goto _resume
		}
	_test_eof:
		{
		}
		if p == eof {
			if _myaSM_eof_trans[cs] > 0 {
				_trans = int(_myaSM_eof_trans[cs] - 1)
				goto _eof_trans
			}
		}

	}

	_ = act // needed by Ragel, but unused
}
