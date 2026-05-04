package project

import "testing"

func TestParseBuildFeatures(t *testing.T) {
	tests := []struct {
		name string
		body string
		want BuildFeatures
	}{
		{
			name: "all enabled",
			body: `
android {
    buildFeatures {
        compose = true
        viewBinding = true
        dataBinding = true
        buildConfig = true
    }
}
`,
			want: BuildFeatures{Compose: true, ViewBinding: true, DataBinding: true, BuildConfig: true},
		},
		{
			name: "compose only",
			body: `
android {
    buildFeatures {
        compose = true
    }
}
`,
			want: BuildFeatures{Compose: true},
		},
		{
			name: "explicit false values",
			body: `
android {
    buildFeatures {
        compose = false
        viewBinding = true
    }
}
`,
			want: BuildFeatures{ViewBinding: true},
		},
		{
			name: "no buildFeatures block",
			body: `
android {
    namespace = "com.example"
}
`,
			want: BuildFeatures{},
		},
		{
			name: "empty buildFeatures block",
			body: `
android {
    buildFeatures {
    }
}
`,
			want: BuildFeatures{},
		},
		{
			name: "viewBinding and dataBinding",
			body: `
android {
    buildFeatures {
        viewBinding = true
        dataBinding = true
    }
}
`,
			want: BuildFeatures{ViewBinding: true, DataBinding: true},
		},
		{
			name: "buildConfig only",
			body: `
android {
    buildFeatures {
        buildConfig = true
    }
}
`,
			want: BuildFeatures{BuildConfig: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseBuildFeatures(tt.body)
			if got != tt.want {
				t.Errorf("parseBuildFeatures() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
