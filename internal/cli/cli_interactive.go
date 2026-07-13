package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/rabesss/impartus-cli/internal/client"
	"github.com/rabesss/impartus-cli/internal/config"
)

func runInteractive() error {
	if err := ensureFFmpeg(); err != nil {
		return err
	}

	ctx := context.Background()
	cfg, apiClient, err := initClient(ctx)
	if err != nil {
		return err
	}

	fmt.Println("Impartus Video Downloader")
	fmt.Println("If you are facing any issues, please check the section at https://github.com/rabesss/impartus-cli#faqtroubleshooting")
	fmt.Println()

	course, err := selectCourseInteractive(ctx, cfg, apiClient)
	if err != nil {
		return err
	}

	selected, err := filterLecturesInteractive(ctx, cfg, apiClient, course)
	if err != nil {
		return err
	}

	_, err = downloadLectures(ctx, cfg, apiClient, selected, humanDownloadPresentation())
	return err
}

func selectCourseInteractive(ctx context.Context, cfg *config.Config, apiClient *client.Client) (*client.Course, error) {
	courses, err := apiClient.GetCourses(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if len(courses) == 0 {
		return nil, errors.New("no courses available")
	}

	for i, course := range courses {
		fmt.Printf("%3d %s\n", i+1, course.SubjectName)
	}

	reader := bufio.NewReader(os.Stdin)
	courseIndex, err := promptInt(reader, "Enter course number: ", 1, len(courses))
	if err != nil {
		return nil, err
	}

	return &courses[courseIndex-1], nil
}

func filterLecturesInteractive(ctx context.Context, cfg *config.Config, apiClient *client.Client, course *client.Course) (client.Lectures, error) {
	lectures, err := apiClient.GetLectures(ctx, cfg, client.Course{SubjectID: course.SubjectID, SessionID: course.SessionID})
	if err != nil {
		return nil, err
	}

	reversed := lectures.Reverse()
	for i, lecture := range reversed {
		fmt.Printf("%3d) LEC %3d %s\n", i+1, lecture.SeqNo, lecture.Topic)
	}

	reader := bufio.NewReader(os.Stdin)
	start, err := promptInt(reader, "Enter start lecture index: ", 1, len(reversed))
	if err != nil {
		return nil, err
	}
	end, err := promptInt(reader, "Enter end lecture index: ", start, len(reversed))
	if err != nil {
		return nil, err
	}

	skipEmpty, err := promptYesNo(reader, "Skip lectures with titles like 'No class' or 'No lecture'? [Y/n]: ", true)
	if err != nil {
		return nil, err
	}

	skipNoAudio, err := promptYesNo(reader, "Skip lectures without audio track? [Y/n]: ", true)
	if err != nil {
		return nil, err
	}

	selected := append(client.Lectures(nil), reversed[start-1:end]...)

	selected, emptyFiltered, noaudioFiltered := applyLectureFilters(selected, skipEmpty, skipNoAudio)

	if len(selected) == 0 {
		return nil, buildNoLecturesError(emptyFiltered, noaudioFiltered)
	}

	return selected, nil
}

func applyLectureFilters(lectures client.Lectures, skipEmpty, skipNoAudio bool) (client.Lectures, int, int) {
	emptyFiltered := 0
	noaudioFiltered := 0

	if skipEmpty {
		before := len(lectures)
		lectures = filterEmptyLectures(lectures)
		emptyFiltered = before - len(lectures)
	}
	if skipNoAudio {
		before := len(lectures)
		lectures = lectures.FilterNoAudio()
		noaudioFiltered = before - len(lectures)
	}

	return lectures, emptyFiltered, noaudioFiltered
}

func buildNoLecturesError(emptyFiltered, noaudioFiltered int) error {
	var reasons []string
	if emptyFiltered > 0 {
		reasons = append(reasons, fmt.Sprintf("%d empty", emptyFiltered))
	}
	if noaudioFiltered > 0 {
		reasons = append(reasons, fmt.Sprintf("%d noaudio", noaudioFiltered))
	}
	return fmt.Errorf("no lectures remaining after filtering: %s filtered out", strings.Join(reasons, ", "))
}

func promptInt(reader *bufio.Reader, prompt string, min, max int) (int, error) {
	for {
		fmt.Print(prompt)
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				line = strings.TrimSpace(line)
			} else {
				return 0, err
			}
		}

		value, convErr := strconv.Atoi(strings.TrimSpace(line))
		if convErr != nil || value < min || value > max {
			fmt.Printf("Enter a number between %d and %d\n", min, max)
			if errors.Is(err, io.EOF) {
				return 0, errors.New("invalid input")
			}
			continue
		}

		return value, nil
	}
}

func promptYesNo(reader *bufio.Reader, prompt string, defaultYes bool) (bool, error) {
	for {
		fmt.Print(prompt)
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return false, err
		}

		value := strings.ToLower(strings.TrimSpace(line))
		switch value {
		case "", "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			if errors.Is(err, io.EOF) {
				if defaultYes {
					return true, nil
				}
				return false, nil
			}
			fmt.Println("Enter y or n")
		}
	}
}
