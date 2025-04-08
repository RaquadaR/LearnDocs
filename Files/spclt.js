// src/index.js
import React from 'react';
import ReactDOM from 'react-dom';
import { Provider } from 'react-redux';
import store from './store';
import App from './App';

ReactDOM.render(
  <Provider store={store}>
    <App />
  </Provider>,
  document.getElementById('root')
);

// src/store.js
import { configureStore } from '@reduxjs/toolkit';
import usersReducer from './features/users/usersSlice';

export default configureStore({
  reducer: {
    users: usersReducer,
  },
});

// src/features/users/usersSlice.js
import { createSlice, createAsyncThunk } from '@reduxjs/toolkit';
import axios from 'axios';

export const fetchUsers = createAsyncThunk('users/fetchUsers', async () => {
  const response = await axios.get('https://jsonplaceholder.typicode.com/users');
  return response.data;
});

const usersSlice = createSlice({
  name: 'users',
  initialState: {
    users: [],
    status: 'idle',
    error: null,
  },
  reducers: {},
  extraReducers: (builder) => {
    builder
      .addCase(fetchUsers.pending, (state) => {
        state.status = 'loading';
      })
      .addCase(fetchUsers.fulfilled, (state, action) => {
        state.status = 'succeeded';
        state.users = action.payload;
      })
      .addCase(fetchUsers.rejected, (state, action) => {
        state.status = 'failed';
        state.error = action.error.message;
      });
  },
});

export default usersSlice.reducer;

// src/App.jsx
import React from 'react';
import { BrowserRouter as Router, Routes, Route } from 'react-router-dom';
import UsersTable from './components/UsersTable';
import UserDetail from './components/UserDetail';

function App() {
  return (
    <Router>
      <div style={{ padding: '20px' }}>
        <h1>User Management</h1>
        <Routes>
          <Route path="/" element={<UsersTable />} />
          <Route path="/user/:id" element={<UserDetail />} />
        </Routes>
      </div>
    </Router>
  );
}

export default App;

// src/components/UsersTable.jsx
import React, { useEffect } from 'react';
import { useSelector, useDispatch } from 'react-redux';
import { useNavigate } from 'react-router-dom';
import { fetchUsers } from '../features/users/usersSlice';
import Table from '@mui/material/Table';
import TableBody from '@mui/material/TableBody';
import TableCell from '@mui/material/TableCell';
import TableContainer from '@mui/material/TableContainer';
import TableHead from '@mui/material/TableHead';
import TableRow from '@mui/material/TableRow';
import Paper from '@mui/material/Paper';
import Button from '@mui/material/Button';
import CircularProgress from '@mui/material/CircularProgress';
import Typography from '@mui/material/Typography';

function UsersTable() {
  const dispatch = useDispatch();
  const navigate = useNavigate();
  const { users, status, error } = useSelector((state) => state.users);

  useEffect(() => {
    if (status === 'idle') {
      dispatch(fetchUsers());
    }
  }, [status, dispatch]);

  const handleViewDetails = (id) => {
    navigate(`/user/${id}`);
  };

  if (status === 'loading') {
    return <CircularProgress />;
  }

  if (status === 'failed') {
    return <Typography color="error">Error: {error}</Typography>;
  }

  return (
    <TableContainer component={Paper}>
      <Table sx={{ minWidth: 650 }} aria-label="users table">
        <TableHead>
          <TableRow>
            <TableCell>Name</TableCell>
            <TableCell>Email</TableCell>
            <TableCell>Company</TableCell>
            <TableCell>Actions</TableCell>
          </TableRow>
        </TableHead>
        <TableBody>
          {users.map((user) => (
            <TableRow
              key={user.id}
              sx={{ '&:last-child td, &:last-child th': { border: 0 } }}
            >
              <TableCell component="th" scope="row">
                {user.name}
              </TableCell>
              <TableCell>{user.email}</TableCell>
              <TableCell>{user.company.name}</TableCell>
              <TableCell>
                <Button
                  variant="contained"
                  color="primary"
                  onClick={() => handleViewDetails(user.id)}
                >
                  View Details
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </TableContainer>
  );
}

export default UsersTable;

// src/components/UserDetail.jsx
import React from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { useSelector } from 'react-redux';
import { Typography, Button, Paper } from '@mui/material';

function UserDetail() {
  const { id } = useParams();
  const navigate = useNavigate();
  const users = useSelector((state) => state.users.users);
  const user = users.find((u) => u.id === parseInt(id));

  if (!user) {
    return <Typography>User not found</Typography>;
  }

  return (
    <Paper sx={{ p: 3, maxWidth: 600, margin: '0 auto' }}>
      <Typography variant="h4" gutterBottom>
        User Details
      </Typography>
      <Typography variant="h6">Name: {user.name}</Typography>
      <Typography variant="body1">Email: {user.email}</Typography>
      <Typography variant="body1">Company: {user.company.name}</Typography>
      <Typography variant="body1">Phone: {user.phone}</Typography>
      <Typography variant="body1">
        Address: {user.address.street}, {user.address.city}
      </Typography>
      <Button
        variant="contained"
        color="primary"
        onClick={() => navigate('/')}
        sx={{ mt: 2 }}
      >
        Back to List
      </Button>
    </Paper>
  );
}

export default UserDetail;