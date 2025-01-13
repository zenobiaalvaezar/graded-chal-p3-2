package handlers

import (
	"context"
	"time"

	"gobook/auth"
	pb "gobook/gobook/proto"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	grpcstatus "google.golang.org/grpc/status"
)

func (s *Server) AddBook(ctx context.Context, req *pb.AddBookRequest) (*pb.AddBookResponse, error) {
	if err := auth.Authenticate(ctx); err != nil {
		return nil, err
	}
	_, err := s.DB.Exec("INSERT INTO books (id, title, author, published_date, status) VALUES ($1, $2, $3, $4, $5)",
		req.Book.Id, req.Book.Title, req.Book.Author, req.Book.PublishedDate, req.Book.Status)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to add book: %v", err)
	}
	return &pb.AddBookResponse{Message: "Book added successfully"}, nil
}
func (s *Server) ListBooks(ctx context.Context, req *pb.ListBooksRequest) (*pb.ListBooksResponse, error) {
	if err := auth.Authenticate(ctx); err != nil {
		return nil, err
	}

	rows, err := s.DB.Query("SELECT id, title, author, published_date, status FROM books")
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to fetch books: %v", err)
	}
	defer rows.Close()

	var books []*pb.Book
	for rows.Next() {
		var book pb.Book
		if err := rows.Scan(&book.Id, &book.Title, &book.Author, &book.PublishedDate, &book.Status); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to parse book: %v", err)
		}
		books = append(books, &book)
	}

	return &pb.ListBooksResponse{Books: books}, nil
}

func (s *Server) DeleteBook(ctx context.Context, req *pb.DeleteBookRequest) (*pb.DeleteBookResponse, error) {
	if err := auth.Authenticate(ctx); err != nil {
		return nil, err
	}

	_, err := s.DB.Exec("DELETE FROM books WHERE id = $1", req.Id)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete book: %v", err)
	}

	return &pb.DeleteBookResponse{Message: "Book deleted successfully"}, nil
}
func (s *Server) UpdateBook(ctx context.Context, req *pb.UpdateBookRequest) (*pb.UpdateBookResponse, error) {
	if err := auth.Authenticate(ctx); err != nil {
		return nil, err
	}

	_, err := s.DB.Exec("UPDATE books SET title = $1, author = $2, published_date = $3, status = $4 WHERE id = $5",
		req.Book.Title, req.Book.Author, req.Book.PublishedDate, req.Book.Status, req.Book.Id)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update book: %v", err)
	}

	return &pb.UpdateBookResponse{Message: "Book updated successfully"}, nil
}

func (s *Server) BorrowBook(ctx context.Context, req *pb.BorrowBookRequest) (*pb.BorrowBookResponse, error) {
	if err := auth.Authenticate(ctx); err != nil {
		return nil, err
	}
	var status string
	err := s.DB.QueryRow("SELECT status FROM books WHERE id = $1", req.BookId).Scan(&status)
	if err != nil {
		return nil, grpcstatus.Errorf(codes.Internal, "failed to query book status: %v", err)
	}
	if status != "Available" {
		return nil, grpcstatus.Errorf(codes.FailedPrecondition, "book is not available")
	}
	_, err = s.DB.Exec("UPDATE books SET status = $1 WHERE id = $2", "Borrowed", req.BookId)
	if err != nil {
		return nil, grpcstatus.Errorf(codes.Internal, "failed to update book status: %v", err)
	}
	_, err = s.DB.Exec("INSERT INTO borrowedbooks (id, book_id, user_id, borrowed_date) VALUES ($1, $2, $3, $4)",
		req.BorrowId, req.BookId, req.UserId, time.Now())
	if err != nil {
		return nil, grpcstatus.Errorf(codes.Internal, "failed to record borrowing: %v", err)
	}
	return &pb.BorrowBookResponse{Message: "Book borrowed successfully"}, nil
}
func (s *Server) ReturnBook(ctx context.Context, req *pb.ReturnBookRequest) (*pb.ReturnBookResponse, error) {
	if err := auth.Authenticate(ctx); err != nil {
		return nil, err
	}

	// Update status buku menjadi "Available"
	_, err := s.DB.Exec("UPDATE books SET status = $1 WHERE id = $2", "Available", req.BookId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update book status: %v", err)
	}

	// Update return_date pada tabel borrowedbooks
	_, err = s.DB.Exec("UPDATE borrowedbooks SET return_date = $1 WHERE book_id = $2 AND user_id = $3",
		time.Now(), req.BookId, req.UserId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to record return: %v", err)
	}

	return &pb.ReturnBookResponse{Message: "Book returned successfully"}, nil
}
