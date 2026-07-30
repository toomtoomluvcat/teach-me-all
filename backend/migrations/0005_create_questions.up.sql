CREATE TABLE questions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    content VARCHAR(100),

    exam_id UUID,
    FOREIGN KEY (exam_id) REFERENCES exams(id) ON DELETE CASCADE
)
