package service_test

import (
	"context"
	"sort"
	"sync"
	"time"

	"api-student/app/model"
	"api-student/app/repository"
)

// Pastikan di waktu compile bahwa fake memenuhi interface yang ditiru.
// Bila suatu hari interface berubah, baris ini yang pertama kali gagal,
// bukan test yang memakainya.
var _ repository.StudentRepository = (*fakeStudentRepo)(nil)

// fakeStudentRepo menyimpan mahasiswa di memory, menggantikan PostgreSQL.
//
// Mutex diperlukan karena Fiber melayani request di goroutine yang berbeda.
type fakeStudentRepo struct {
	repository.StudentRepository // method lain: panic bila terpanggil

	mu       sync.Mutex
	nextID   int
	students map[int]model.Student
}

func newFakeStudentRepo() *fakeStudentRepo {
	return &fakeStudentRepo{students: map[int]model.Student{}}
}

func (f *fakeStudentRepo) WithTx(_ repository.DBTX) repository.StudentRepository {
	return f
}

func (f *fakeStudentRepo) FindByID(_ context.Context, id int) (model.Student, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	s, ok := f.students[id]
	if !ok {
		return model.Student{}, repository.ErrNotFound
	}
	return s, nil
}

// FindAfterCursor meniru satu halaman keyset pagination: urutan id menurun,
// lalu dipotong sebanyak limit+1 — persis kontrak yang diharapkan service.
func (f *fakeStudentRepo) FindAfterCursor(_ context.Context, q model.CursorQuery) ([]model.Student, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	ids := make([]int, 0, len(f.students))
	for id := range f.students {
		ids = append(ids, id)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(ids)))

	result := []model.Student{}
	for _, id := range ids {
		result = append(result, f.students[id])
		if len(result) == q.Limit+1 {
			break
		}
	}
	return result, nil
}

func (f *fakeStudentRepo) Create(_ context.Context, s model.Student) (model.Student, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, existing := range f.students {
		if existing.NIM == s.NIM {
			return model.Student{}, repository.ErrDuplicate
		}
	}

	f.nextID++
	s.ID = f.nextID
	s.CreatedAt = time.Now()
	f.students[s.ID] = s
	return s, nil
}

func (f *fakeStudentRepo) Update(_ context.Context, s model.Student) (model.Student, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.students[s.ID]; !ok {
		return model.Student{}, repository.ErrNotFound
	}
	f.students[s.ID] = s
	return s, nil
}

func (f *fakeStudentRepo) Delete(_ context.Context, id int) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.students[id]; !ok {
		return repository.ErrNotFound
	}
	delete(f.students, id)
	return nil
}
