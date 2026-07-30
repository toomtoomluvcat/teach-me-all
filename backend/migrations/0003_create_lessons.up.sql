CREATE TABLE lessons  (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title VARCHAR(100),

    course_id UUID,
    FOREIGN KEY (course_id) REFERENCES courses(id) ON DELETE CASCADE
)