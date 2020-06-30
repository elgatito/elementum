// Implements the libtorrent field interface
// bool get_bit(int index) const
// {
//     TORRENT_ASSERT(index >= 0);
//     TORRENT_ASSERT(index < m_size);
//     return (m_bytes[index / 8] & (0x80 >> (index & 7))) != 0;
// }

// // set bit at ``index`` to 0 (clear_bit) or 1 (set_bit).
// void clear_bit(int index)
// {
//     TORRENT_ASSERT(index >= 0);
//     TORRENT_ASSERT(index < m_size);
//     m_bytes[index / 8] &= ~(0x80 >> (index & 7));
// }
// void set_bit(int index)
// {
//     TORRENT_ASSERT(index >= 0);
//     TORRENT_ASSERT(index < m_size);
//     m_bytes[index / 8] |= (0x80 >> (index & 7));
// }

package bittorrent

import "bytes"

// Bitfield ...
type Bitfield []byte

// SetBit ...
func (b Bitfield) SetBit(idx int, value bool) {
	if value {
		b[uint(idx/8)] |= 0x80 >> (uint(idx) & 7)
	} else {
		b[uint(idx/8)] &^= 0x80 >> (uint(idx) & 7)
	}
}

// GetBit ...
func (b Bitfield) GetBit(idx int) bool {
	return b[uint(idx/8)]&(0x80>>(uint(idx)&7)) != 0
}

// String ...
func (b Bitfield) String() string {
	out := bytes.Buffer{}
	out.Grow(len(b) * 8)
	for i := 0; i < len(b)*8; i++ {
		if b.GetBit(i) {
			out.WriteRune('1')
		} else {
			out.WriteRune('0')
		}
	}
	return out.String()
}
