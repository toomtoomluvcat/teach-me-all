CREATE TABLE exams (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title VARCHAR(100),
    has_taken BOOLEAN NOT NULL DEFAULT FALSE,

    lesson_id UUID,
    FOREIGN KEY (lesson_id) REFERENCES lessons(id) ON DELETE CASCADE
)