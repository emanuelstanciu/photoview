import React from 'react'
import PropTypes from 'prop-types'
import styled from 'styled-components'
import { Loader } from 'semantic-ui-react'
import { AlbumBox } from './AlbumBox'

const Container = styled.div`
  margin: 20px -10px;
  position: relative;
`

const AlbumBoxes = ({ loading, error, albums, getCustomLink }) => {
  if (error) return <div>Error {error.message}</div>

  let albumElements = []

  if (albums) {
    albumElements = albums.map(album => (
      <AlbumBox
        key={album.id}
        album={album}
        customLink={getCustomLink ? getCustomLink(album.id) : null}
      />
    ))
  } else {
    for (let i = 0; i < 4; i++) {
      albumElements.push(<AlbumBox key={i} />)
    }
  }

  return (
    <Container>
      {/* <Loader active={loading}>Loading albums</Loader> */}
      {albumElements}
    </Container>
  )
}

AlbumBoxes.propTypes = {
  loading: PropTypes.bool.isRequired,
  error: PropTypes.object,
  albums: PropTypes.array,
  getCustomLink: PropTypes.func,
}

export default AlbumBoxes
