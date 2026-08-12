package client

import (
	"context"
	"errors"
	"fmt"

	"github.com/rabesss/impartus-cli/internal/config"
)

// CourseCatalog is the course-listing seam used to recover institute scope
// when upstream lecture records omit it.
type CourseCatalog interface {
	GetCourses(context.Context, *config.Config) (Courses, error)
}

// ResolveLectureScope validates the selected subject/session against positive
// upstream values, then fills omitted scope from the unambiguous selection.
func ResolveLectureScope(
	ctx context.Context,
	cfg *config.Config,
	catalog CourseCatalog,
	lectures Lectures,
	subjectID, sessionID int,
) error {
	knownInstitutes, missingInstitute, err := inspectSelectedCourseScope(lectures, subjectID, sessionID)
	if err != nil {
		return err
	}
	instituteID, err := uniqueInstituteID(knownInstitutes)
	if err != nil {
		return err
	}
	if missingInstitute && instituteID == 0 {
		instituteID, err = resolveInstituteFromCatalog(ctx, cfg, catalog, subjectID, sessionID)
		if err != nil {
			return err
		}
	}
	if missingInstitute && instituteID == 0 {
		return fmt.Errorf("cannot resolve institute scope for subject=%d session=%d", subjectID, sessionID)
	}
	for index := range lectures {
		lectures[index].SubjectID = subjectID
		lectures[index].SessionID = sessionID
		if lectures[index].InstituteID == 0 {
			lectures[index].InstituteID = instituteID
		}
	}
	return nil
}

func inspectSelectedCourseScope(lectures Lectures, subjectID, sessionID int) (map[int]struct{}, bool, error) {
	knownInstitutes := make(map[int]struct{})
	missingInstitute := false
	for index := range lectures {
		if lectures[index].SubjectID > 0 && lectures[index].SubjectID != subjectID {
			return nil, false, fmt.Errorf("subject scope mismatch for lecture %d", lectures[index].TTID)
		}
		if lectures[index].SessionID > 0 && lectures[index].SessionID != sessionID {
			return nil, false, fmt.Errorf("session scope mismatch for lecture %d", lectures[index].TTID)
		}
		if lectures[index].InstituteID > 0 {
			knownInstitutes[lectures[index].InstituteID] = struct{}{}
		} else {
			missingInstitute = true
		}
	}
	return knownInstitutes, missingInstitute, nil
}

func resolveInstituteFromCatalog(
	ctx context.Context,
	cfg *config.Config,
	catalog CourseCatalog,
	subjectID, sessionID int,
) (int, error) {
	if catalog == nil {
		return 0, errors.New("cannot resolve missing institute scope: course catalog is unavailable")
	}
	courses, err := catalog.GetCourses(ctx, cfg)
	if err != nil {
		return 0, fmt.Errorf("resolve missing institute scope from course catalog: %w", err)
	}
	institutes := make(map[int]struct{})
	for _, course := range courses {
		if course.SubjectID == subjectID && course.SessionID == sessionID && course.InstituteID > 0 {
			institutes[course.InstituteID] = struct{}{}
		}
	}
	return uniqueInstituteID(institutes)
}

func uniqueInstituteID(institutes map[int]struct{}) (int, error) {
	if len(institutes) > 1 {
		return 0, errors.New("ambiguous institute scope for selected lectures")
	}
	for instituteID := range institutes {
		return instituteID, nil
	}
	return 0, nil
}
