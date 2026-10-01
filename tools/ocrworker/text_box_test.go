package ocrworker

import "testing"

func TestFindTextBoxInTSVReturnsPhraseBounds(t *testing.T) {
	tsv := "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n" +
		"5\t1\t2\t1\t3\t1\t10\t20\t30\t12\t90\tminta\n" +
		"5\t1\t2\t1\t3\t2\t45\t20\t25\t12\t80\tparty,\n" +
		"5\t1\t2\t1\t3\t3\t80\t20\t20\t12\t70\tom!\n" +
		"5\t1\t2\t1\t4\t1\t10\t40\t30\t12\t90\tother\n"
	match := FindTextBoxInTSV(tsv, "Minta party om")
	if match == nil {
		t.Fatal("expected phrase match")
	}
	if match.X != 10 || match.Y != 20 || match.Width != 90 || match.Height != 12 {
		t.Fatalf("unexpected bounds: %+v", match)
	}
	if match.Confidence != 80 {
		t.Fatalf("unexpected confidence: %.1f", match.Confidence)
	}
}

func TestFindTextBoxInTSVDoesNotJoinLines(t *testing.T) {
	tsv := "header\n5\t1\t1\t1\t1\t1\t0\t0\t20\t10\t90\tminta\n5\t1\t1\t1\t2\t1\t0\t20\t20\t10\t90\tparty\n"
	if match := FindTextBoxInTSV(tsv, "minta party"); match != nil {
		t.Fatalf("unexpected cross-line match: %+v", match)
	}
}

func TestFindTextBoxInTSVSupportsWindowsLineEndings(t *testing.T) {
	tsv := "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\r\n" +
		"5\t1\t1\t1\t1\t1\t10\t20\t30\t12\t90\tGianluigiGufron:\r\n" +
		"5\t1\t1\t1\t1\t2\t45\t20\t25\t12\t95\tparty\r\n" +
		"5\t1\t1\t1\t1\t3\t80\t20\t20\t12\t95\tbank\r\n"
	match := FindTextBoxInTSV(tsv, "party bank")
	if match == nil {
		t.Fatal("expected phrase match with Windows line endings")
	}
	if match.X != 45 || match.Width != 55 {
		t.Fatalf("unexpected phrase bounds: %+v", match)
	}
}

func TestFindTextBoxInTSVFindsCharacterNameBelowChatBubble(t *testing.T) {
	tsv := "level\tpage_num\tblock_num\tpar_num\tline_num\tword_num\tleft\ttop\twidth\theight\tconf\ttext\n" +
		"5\t1\t1\t1\t1\t1\t10\t20\t120\t12\t90\tGianluigiGufron:\n" +
		"5\t1\t1\t1\t1\t2\t135\t20\t45\t12\t90\tparty\n" +
		"5\t1\t1\t1\t1\t3\t185\t20\t30\t12\t90\tom\n" +
		"5\t1\t1\t1\t2\t1\t80\t40\t100\t12\t95\tGianluigiGufron\n" +
		"5\t1\t1\t1\t3\t1\t80\t110\t100\t12\t95\tNearbyName\n"

	match := FindTextBoxInTSV(tsv, "party om")
	if match == nil || match.FollowingLine == nil {
		t.Fatal("expected phrase and following character-name line")
	}
	if match.FollowingLine.Text != "GianluigiGufron" || match.FollowingLine.X != 80 {
		t.Fatalf("following line = %+v, want centered name below bubble", match.FollowingLine)
	}
}
